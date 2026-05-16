package canring

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

const (
	superblockSize  = 4096
	superblockCount = 2
	chunkHeaderSize = 64
	recordSize      = 48
	minChunkBytes   = 64 * 1024

	superblockMagic = "MECANRNG"
	chunkMagic      = uint32(0x4d43524b)
	version         = uint16(1)
)

// Config describes a fixed-size CAN capture ring.
type Config struct {
	Path        string
	SizeBytes   int64
	ChunkBytes  int64
	Interface   string
	SyncOnChunk bool
}

// Stats exposes writer progress without requiring callers to parse the file.
type Stats struct {
	Path         string
	SizeBytes    int64
	ChunkBytes   int64
	ChunkCount   uint64
	NextChunk    uint64
	TotalChunks  uint64
	TotalRecords uint64
}

// Record is a decoded CAN frame from the fixed-size ring.
type Record struct {
	Seq          uint64
	Time         time.Time
	ElapsedNanos int64
	ID           uint32
	DLC          uint8
	Data         [8]byte
	Interface    string
	Chunk        uint64
}

// Snapshot is a point-in-time read of committed, checksum-valid chunks.
type Snapshot struct {
	Stats       Stats
	Records     []Record
	ValidChunks int
}

// MergeRecords combines records from the primary hot ring and fallback ring.
// Mirrored frames are de-duplicated by CAN identity and timestamp, preferring
// the primary record when both rings contain the same frame.
func MergeRecords(primary, fallback []Record, limit int) []Record {
	merged := make([]Record, 0, len(primary)+len(fallback))
	seen := make(map[recordKey]struct{}, len(primary)+len(fallback))
	for _, record := range primary {
		key := newRecordKey(record)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, record)
	}
	for _, record := range fallback {
		key := newRecordKey(record)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, record)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].Time.Equal(merged[j].Time) {
			return merged[i].Time.Before(merged[j].Time)
		}
		if merged[i].Seq != merged[j].Seq {
			return merged[i].Seq < merged[j].Seq
		}
		if merged[i].ID != merged[j].ID {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].Interface < merged[j].Interface
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[len(merged)-limit:]
	}
	return merged
}

type recordKey struct {
	wallNS int64
	id     uint32
	dlc    uint8
	data   [8]byte
	iface  string
}

func newRecordKey(record Record) recordKey {
	return recordKey{
		wallNS: record.Time.UnixNano(),
		id:     record.ID,
		dlc:    record.DLC,
		data:   record.Data,
		iface:  record.Interface,
	}
}

type header struct {
	Generation   uint64
	FileBytes    uint64
	ChunkBytes   uint64
	ChunkCount   uint64
	NextChunk    uint64
	TotalChunks  uint64
	TotalRecords uint64
	Interface    string
}

// Writer appends CAN frames to a fixed-size chunked ring file.
type Writer struct {
	file       *os.File
	path       string
	chunkBytes int64
	chunkCount uint64
	nextChunk  uint64
	syncChunk  bool
	iface      string
	start      time.Time

	buf          []byte
	records      int
	firstSeq     uint64
	lastSeq      uint64
	firstWallNS  int64
	lastWallNS   int64
	totalChunks  uint64
	totalRecords uint64
	generation   uint64
	closed       bool
}

// Reader reads committed chunks from a CAN capture ring without mutating it.
type Reader struct {
	file *os.File
	path string
	h    header
}

// OpenWriter opens or initializes a ring file. Existing valid metadata is used
// to resume at the next chunk; incompatible files are reinitialized in place.
func OpenWriter(cfg Config) (*Writer, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("can ring path is required")
	}
	if cfg.SizeBytes <= 0 {
		return nil, errors.New("can ring size must be positive")
	}
	if cfg.ChunkBytes < minChunkBytes {
		return nil, fmt.Errorf("can ring chunk size must be at least %d bytes", minChunkBytes)
	}
	usable := cfg.SizeBytes - superblockCount*superblockSize
	if usable < cfg.ChunkBytes*2 {
		return nil, errors.New("can ring size must hold at least two data chunks")
	}
	chunkCount := uint64(usable / cfg.ChunkBytes)
	fileBytes := int64(superblockCount*superblockSize) + int64(chunkCount)*cfg.ChunkBytes

	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(cfg.Path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(fileBytes); err != nil {
		_ = f.Close()
		return nil, err
	}

	w := &Writer{
		file:       f,
		path:       cfg.Path,
		chunkBytes: cfg.ChunkBytes,
		chunkCount: chunkCount,
		syncChunk:  cfg.SyncOnChunk,
		iface:      cfg.Interface,
		start:      time.Now(),
		buf:        make([]byte, cfg.ChunkBytes),
	}
	if h, ok := readLatestHeader(f); ok {
		w.generation = h.Generation
		if h.FileBytes == uint64(fileBytes) && h.ChunkBytes == uint64(cfg.ChunkBytes) && h.ChunkCount == chunkCount {
			w.nextChunk = h.NextChunk % chunkCount
			w.totalChunks = h.TotalChunks
			w.totalRecords = h.TotalRecords
			if w.iface == "" {
				w.iface = h.Interface
			}
			return w, nil
		}
	}
	if err := clearDataRegion(f, fileBytes); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := w.writeHeader(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// Append buffers a received frame. Disk writes happen only when a chunk fills
// or when Close flushes the final partial chunk.
func (w *Writer) Append(f canopen.Frame, ts time.Time) error {
	if w.closed {
		return errors.New("can ring writer is closed")
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if w.records == 0 {
		clear(w.buf)
		w.firstSeq = w.totalRecords
		w.firstWallNS = ts.UnixNano()
	}
	offset := chunkHeaderSize + w.records*recordSize
	encodeRecord(w.buf[offset:offset+recordSize], w.totalRecords, ts, time.Since(w.start), f)
	w.lastSeq = w.totalRecords
	w.lastWallNS = ts.UnixNano()
	w.records++
	w.totalRecords++
	if w.records >= w.maxRecordsPerChunk() {
		return w.flushChunk()
	}
	return nil
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	var err error
	if w.records > 0 {
		err = w.flushChunk()
	}
	if closeErr := w.file.Close(); err == nil {
		err = closeErr
	}
	w.closed = true
	return err
}

func (w *Writer) Stats() Stats {
	return Stats{
		Path:         w.path,
		SizeBytes:    int64(superblockCount*superblockSize) + int64(w.chunkCount)*w.chunkBytes,
		ChunkBytes:   w.chunkBytes,
		ChunkCount:   w.chunkCount,
		NextChunk:    w.nextChunk,
		TotalChunks:  w.totalChunks,
		TotalRecords: w.totalRecords,
	}
}

// OpenReader opens an existing ring for read-only replay.
func OpenReader(path string) (*Reader, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("can ring path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	h, ok := readLatestHeader(f)
	if !ok {
		_ = f.Close()
		return nil, errors.New("can ring header is not readable")
	}
	return &Reader{file: f, path: path, h: h}, nil
}

func (r *Reader) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

func (r *Reader) Stats() Stats {
	return statsFromHeader(r.path, r.h)
}

// Snapshot returns records from checksum-valid committed chunks, ordered by
// sequence. A positive limit keeps the most recent records.
func (r *Reader) Snapshot(limit int) (Snapshot, error) {
	h, ok := readLatestHeader(r.file)
	if !ok {
		return Snapshot{}, errors.New("can ring header is not readable")
	}
	r.h = h
	chunks, err := r.snapshotChunks(limit)
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].firstSeq == chunks[j].firstSeq {
			return chunks[i].chunkSeq < chunks[j].chunkSeq
		}
		return chunks[i].firstSeq < chunks[j].firstSeq
	})
	records := make([]Record, 0)
	for _, chunk := range chunks {
		for i := 0; i < chunk.records; i++ {
			offset := i * recordSize
			records = append(records, decodeRecord(chunk.payload[offset:offset+recordSize], h.Interface, chunk.chunkSeq))
		}
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return Snapshot{
		Stats:       statsFromHeader(r.path, h),
		Records:     records,
		ValidChunks: len(chunks),
	}, nil
}

func (r *Reader) snapshotChunks(limit int) ([]ringChunk, error) {
	if r.h.ChunkCount == 0 {
		return nil, nil
	}
	if limit <= 0 {
		return r.readAllChunks()
	}
	return r.readRecentChunks(limit)
}

func (r *Reader) readAllChunks() ([]ringChunk, error) {
	maxChunks := r.h.ChunkCount
	if r.h.TotalChunks < maxChunks {
		maxChunks = r.h.TotalChunks
	}
	chunks := make([]ringChunk, 0, maxChunks)
	for i := uint64(0); i < maxChunks; i++ {
		chunk, ok, err := r.readChunk(i)
		if err != nil {
			return nil, err
		}
		if ok {
			chunks = append(chunks, chunk)
		}
	}
	return chunks, nil
}

func clearDataRegion(f *os.File, fileBytes int64) error {
	offset := int64(superblockCount * superblockSize)
	if fileBytes <= offset {
		return nil
	}
	zeros := make([]byte, minChunkBytes)
	for offset < fileBytes {
		n := int64(len(zeros))
		if remaining := fileBytes - offset; remaining < n {
			n = remaining
		}
		if _, err := f.WriteAt(zeros[:n], offset); err != nil {
			return err
		}
		offset += n
	}
	return nil
}

func (r *Reader) readRecentChunks(limit int) ([]ringChunk, error) {
	maxChunks := r.h.ChunkCount
	if r.h.TotalChunks < maxChunks {
		maxChunks = r.h.TotalChunks
	}
	chunks := make([]ringChunk, 0)
	records := 0
	for scanned := uint64(0); scanned < maxChunks; scanned++ {
		index := (r.h.NextChunk + r.h.ChunkCount - 1 - scanned) % r.h.ChunkCount
		chunk, ok, err := r.readChunk(index)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		chunks = append(chunks, chunk)
		records += chunk.records
		if records >= limit {
			break
		}
	}
	return chunks, nil
}

func (r *Reader) readChunk(index uint64) (ringChunk, bool, error) {
	if r.h.ChunkBytes == 0 || r.h.ChunkCount == 0 {
		return ringChunk{}, false, nil
	}
	buf := make([]byte, chunkHeaderSize)
	offset := int64(superblockCount*superblockSize) + int64(index)*int64(r.h.ChunkBytes)
	if _, err := r.file.ReadAt(buf, offset); err != nil {
		if errors.Is(err, io.EOF) {
			return ringChunk{}, false, nil
		}
		return ringChunk{}, false, err
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != chunkMagic {
		return ringChunk{}, false, nil
	}
	if binary.LittleEndian.Uint16(buf[4:6]) != version || binary.LittleEndian.Uint16(buf[6:8]) != chunkHeaderSize {
		return ringChunk{}, false, nil
	}
	records := int(binary.LittleEndian.Uint32(buf[48:52]))
	payloadLen := int(binary.LittleEndian.Uint32(buf[52:56]))
	if records <= 0 || payloadLen != records*recordSize || payloadLen > int(r.h.ChunkBytes)-chunkHeaderSize {
		return ringChunk{}, false, nil
	}
	payload := make([]byte, payloadLen)
	if _, err := r.file.ReadAt(payload, offset+chunkHeaderSize); err != nil {
		return ringChunk{}, false, err
	}
	if crc32.ChecksumIEEE(payload) != binary.LittleEndian.Uint32(buf[56:60]) {
		return ringChunk{}, false, nil
	}
	return ringChunk{
		chunkSeq: binary.LittleEndian.Uint64(buf[8:16]),
		firstSeq: binary.LittleEndian.Uint64(buf[16:24]),
		lastSeq:  binary.LittleEndian.Uint64(buf[24:32]),
		records:  records,
		payload:  payload,
	}, true, nil
}

func (w *Writer) maxRecordsPerChunk() int {
	return int((w.chunkBytes - chunkHeaderSize) / recordSize)
}

func (w *Writer) flushChunk() error {
	if w.records == 0 {
		return nil
	}
	payloadLen := w.records * recordSize
	binary.LittleEndian.PutUint32(w.buf[0:4], chunkMagic)
	binary.LittleEndian.PutUint16(w.buf[4:6], version)
	binary.LittleEndian.PutUint16(w.buf[6:8], chunkHeaderSize)
	binary.LittleEndian.PutUint64(w.buf[8:16], w.totalChunks)
	binary.LittleEndian.PutUint64(w.buf[16:24], w.firstSeq)
	binary.LittleEndian.PutUint64(w.buf[24:32], w.lastSeq)
	binary.LittleEndian.PutUint64(w.buf[32:40], uint64(w.firstWallNS))
	binary.LittleEndian.PutUint64(w.buf[40:48], uint64(w.lastWallNS))
	binary.LittleEndian.PutUint32(w.buf[48:52], uint32(w.records))
	binary.LittleEndian.PutUint32(w.buf[52:56], uint32(payloadLen))
	binary.LittleEndian.PutUint32(w.buf[56:60], crc32.ChecksumIEEE(w.buf[chunkHeaderSize:chunkHeaderSize+payloadLen]))

	offset := int64(superblockCount*superblockSize) + int64(w.nextChunk)*w.chunkBytes
	if _, err := w.file.WriteAt(w.buf, offset); err != nil {
		return err
	}
	w.nextChunk = (w.nextChunk + 1) % w.chunkCount
	w.totalChunks++
	w.records = 0
	if err := w.writeHeader(); err != nil {
		return err
	}
	if w.syncChunk {
		return w.file.Sync()
	}
	return nil
}

func (w *Writer) writeHeader() error {
	w.generation++
	fileBytes := uint64(superblockCount*superblockSize) + w.chunkCount*uint64(w.chunkBytes)
	h := header{
		Generation:   w.generation,
		FileBytes:    fileBytes,
		ChunkBytes:   uint64(w.chunkBytes),
		ChunkCount:   w.chunkCount,
		NextChunk:    w.nextChunk,
		TotalChunks:  w.totalChunks,
		TotalRecords: w.totalRecords,
		Interface:    w.iface,
	}
	buf := encodeHeader(h)
	slot := int64((w.generation % superblockCount) * superblockSize)
	_, err := w.file.WriteAt(buf, slot)
	return err
}

func encodeHeader(h header) []byte {
	buf := make([]byte, superblockSize)
	copy(buf[0:8], superblockMagic)
	binary.LittleEndian.PutUint16(buf[8:10], version)
	binary.LittleEndian.PutUint16(buf[10:12], superblockSize)
	binary.LittleEndian.PutUint64(buf[16:24], h.Generation)
	binary.LittleEndian.PutUint64(buf[24:32], h.FileBytes)
	binary.LittleEndian.PutUint64(buf[32:40], h.ChunkBytes)
	binary.LittleEndian.PutUint64(buf[40:48], h.ChunkCount)
	binary.LittleEndian.PutUint64(buf[48:56], h.NextChunk)
	binary.LittleEndian.PutUint64(buf[56:64], h.TotalChunks)
	binary.LittleEndian.PutUint64(buf[64:72], h.TotalRecords)
	copy(buf[72:104], []byte(h.Interface))
	binary.LittleEndian.PutUint16(buf[104:106], recordSize)
	crc := crc32.ChecksumIEEE(buf[16:])
	binary.LittleEndian.PutUint32(buf[12:16], crc)
	return buf
}

func decodeHeader(buf []byte) (header, bool) {
	if len(buf) != superblockSize || string(buf[0:8]) != superblockMagic {
		return header{}, false
	}
	if binary.LittleEndian.Uint16(buf[8:10]) != version || binary.LittleEndian.Uint16(buf[10:12]) != superblockSize {
		return header{}, false
	}
	want := binary.LittleEndian.Uint32(buf[12:16])
	if crc32.ChecksumIEEE(buf[16:]) != want {
		return header{}, false
	}
	if binary.LittleEndian.Uint16(buf[104:106]) != recordSize {
		return header{}, false
	}
	return header{
		Generation:   binary.LittleEndian.Uint64(buf[16:24]),
		FileBytes:    binary.LittleEndian.Uint64(buf[24:32]),
		ChunkBytes:   binary.LittleEndian.Uint64(buf[32:40]),
		ChunkCount:   binary.LittleEndian.Uint64(buf[40:48]),
		NextChunk:    binary.LittleEndian.Uint64(buf[48:56]),
		TotalChunks:  binary.LittleEndian.Uint64(buf[56:64]),
		TotalRecords: binary.LittleEndian.Uint64(buf[64:72]),
		Interface:    strings.TrimRight(string(buf[72:104]), "\x00"),
	}, true
}

func readLatestHeader(f *os.File) (header, bool) {
	var best header
	found := false
	for i := 0; i < superblockCount; i++ {
		buf := make([]byte, superblockSize)
		if _, err := f.ReadAt(buf, int64(i*superblockSize)); err != nil && !errors.Is(err, io.EOF) {
			continue
		}
		h, ok := decodeHeader(buf)
		if ok && (!found || h.Generation > best.Generation) {
			best = h
			found = true
		}
	}
	return best, found
}

type ringChunk struct {
	chunkSeq uint64
	firstSeq uint64
	lastSeq  uint64
	records  int
	payload  []byte
}

func statsFromHeader(path string, h header) Stats {
	return Stats{
		Path:         path,
		SizeBytes:    int64(h.FileBytes),
		ChunkBytes:   int64(h.ChunkBytes),
		ChunkCount:   h.ChunkCount,
		NextChunk:    h.NextChunk,
		TotalChunks:  h.TotalChunks,
		TotalRecords: h.TotalRecords,
	}
}

func encodeRecord(dst []byte, seq uint64, ts time.Time, elapsed time.Duration, f canopen.Frame) {
	binary.LittleEndian.PutUint64(dst[0:8], seq)
	binary.LittleEndian.PutUint64(dst[8:16], uint64(ts.UnixNano()))
	binary.LittleEndian.PutUint64(dst[16:24], uint64(elapsed.Nanoseconds()))
	binary.LittleEndian.PutUint32(dst[24:28], f.ID)
	dst[28] = f.DLC
	copy(dst[32:40], f.Data[:])
}

func decodeRecord(src []byte, iface string, chunk uint64) Record {
	var data [8]byte
	copy(data[:], src[32:40])
	return Record{
		Seq:          binary.LittleEndian.Uint64(src[0:8]),
		Time:         time.Unix(0, int64(binary.LittleEndian.Uint64(src[8:16]))).UTC(),
		ElapsedNanos: int64(binary.LittleEndian.Uint64(src[16:24])),
		ID:           binary.LittleEndian.Uint32(src[24:28]),
		DLC:          src[28],
		Data:         data,
		Interface:    iface,
		Chunk:        chunk,
	}
}
