package canring

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

func TestWriterWrapsWithoutGrowingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "can.ring")
	cfg := Config{
		Path:        path,
		SizeBytes:   int64(superblockCount*superblockSize) + 2*minChunkBytes,
		ChunkBytes:  minChunkBytes,
		Interface:   "can0",
		SyncOnChunk: false,
	}
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	frame := canopen.Frame{ID: 0x123, DLC: 2, Data: [8]byte{0xAA, 0x55}}
	total := w.maxRecordsPerChunk()*3 + 7
	for i := 0; i < total; i++ {
		if err := w.Append(frame, time.Unix(1700000000, int64(i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != cfg.SizeBytes {
		t.Fatalf("file grew: got %d want %d", info.Size(), cfg.SizeBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h, ok := readLatestHeader(f)
	if !ok {
		t.Fatal("ring header is not readable")
	}
	if h.TotalRecords != uint64(total) {
		t.Fatalf("total records got %d want %d", h.TotalRecords, total)
	}
	if h.TotalChunks < 4 {
		t.Fatalf("expected wrapped chunk commits, got %d", h.TotalChunks)
	}
	if h.Interface != "can0" {
		t.Fatalf("interface got %q", h.Interface)
	}
}

func TestOpenWriterResumesValidRing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.ring")
	cfg := Config{
		Path:       path,
		SizeBytes:  int64(superblockCount*superblockSize) + 2*minChunkBytes,
		ChunkBytes: minChunkBytes,
		Interface:  "can1",
	}
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < w.maxRecordsPerChunk(); i++ {
		if err := w.Append(canopen.Frame{ID: 0x701, DLC: 1, Data: [8]byte{0x05}}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	first := w.Stats()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	w, err = OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	resumed := w.Stats()
	if resumed.TotalRecords != first.TotalRecords {
		t.Fatalf("resume records got %d want %d", resumed.TotalRecords, first.TotalRecords)
	}
	if resumed.NextChunk != first.NextChunk {
		t.Fatalf("resume next chunk got %d want %d", resumed.NextChunk, first.NextChunk)
	}
}

func TestReaderSnapshotsCommittedRecordsInSequenceOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "read.ring")
	cfg := Config{
		Path:       path,
		SizeBytes:  int64(superblockCount*superblockSize) + 2*minChunkBytes,
		ChunkBytes: minChunkBytes,
		Interface:  "can0",
	}
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < w.maxRecordsPerChunk()+3; i++ {
		frame := canopen.Frame{ID: uint32(0x700 + i), DLC: 2, Data: [8]byte{byte(i), 0xA5}}
		if err := w.Append(frame, time.Unix(1700000000, int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	snapshot, err := r.Snapshot(4)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ValidChunks != 2 {
		t.Fatalf("valid chunks = %d", snapshot.ValidChunks)
	}
	if len(snapshot.Records) != 4 {
		t.Fatalf("records = %d", len(snapshot.Records))
	}
	for i := 1; i < len(snapshot.Records); i++ {
		if snapshot.Records[i].Seq <= snapshot.Records[i-1].Seq {
			t.Fatalf("records not ordered: %#v", snapshot.Records)
		}
	}
	last := snapshot.Records[len(snapshot.Records)-1]
	if last.Interface != "can0" || last.ID != uint32(0x700+w.maxRecordsPerChunk()+2) || last.Data[1] != 0xA5 {
		t.Fatalf("unexpected last record: %#v", last)
	}
}

func TestReaderLimitedSnapshotReadsOnlyRecentChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.ring")
	cfg := Config{
		Path:       path,
		SizeBytes:  int64(superblockCount*superblockSize) + 8*minChunkBytes,
		ChunkBytes: minChunkBytes,
		Interface:  "can0",
	}
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	totalChunks := 6
	totalRecords := w.maxRecordsPerChunk()*totalChunks + 9
	for i := 0; i < totalRecords; i++ {
		frame := canopen.Frame{ID: uint32(0x100 + i%0x600), DLC: 1, Data: [8]byte{byte(i)}}
		if err := w.Append(frame, time.Unix(1700000000, int64(i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	snapshot, err := r.Snapshot(5)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ValidChunks != 1 {
		t.Fatalf("valid chunks = %d, want one recent chunk for limited replay", snapshot.ValidChunks)
	}
	if len(snapshot.Records) != 5 {
		t.Fatalf("records = %d", len(snapshot.Records))
	}
	for i, record := range snapshot.Records {
		wantSeq := uint64(totalRecords - 5 + i)
		if record.Seq != wantSeq {
			t.Fatalf("record[%d].Seq = %d want %d; records=%#v", i, record.Seq, wantSeq, snapshot.Records)
		}
	}
}

func TestRejectsUndersizedConfig(t *testing.T) {
	_, err := OpenWriter(Config{
		Path:       filepath.Join(t.TempDir(), "bad.ring"),
		SizeBytes:  1024,
		ChunkBytes: minChunkBytes,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergeRecordsPrefersPrimaryAndFillsFallbackGap(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	duplicateFrame := [8]byte{0x01, 0x02}
	primary := []Record{
		{Seq: 10, Time: t0.Add(20 * time.Millisecond), ElapsedNanos: 20_000_000, ID: 0x701, DLC: 2, Data: duplicateFrame, Interface: "can0", Chunk: 2},
		{Seq: 11, Time: t0.Add(30 * time.Millisecond), ElapsedNanos: 30_000_000, ID: 0x702, DLC: 1, Data: [8]byte{0x05}, Interface: "can0", Chunk: 2},
	}
	fallback := []Record{
		{Seq: 100, Time: t0.Add(10 * time.Millisecond), ElapsedNanos: 10_000_000, ID: 0x700, DLC: 1, Data: [8]byte{0x7f}, Interface: "can0", Chunk: 7},
		{Seq: 101, Time: t0.Add(20 * time.Millisecond), ElapsedNanos: 21_000_000, ID: 0x701, DLC: 2, Data: duplicateFrame, Interface: "can0", Chunk: 7},
	}

	merged := MergeRecords(primary, fallback, 0)
	if len(merged) != 3 {
		t.Fatalf("merged records = %d: %#v", len(merged), merged)
	}
	if merged[0].ID != 0x700 || merged[1].ID != 0x701 || merged[2].ID != 0x702 {
		t.Fatalf("unexpected merge order: %#v", merged)
	}
	if merged[1].Seq != 10 || merged[1].Chunk != 2 {
		t.Fatalf("duplicate should prefer primary record, got %#v", merged[1])
	}
}

func TestMergeRecordsKeepsNewestLimitAfterCombiningSources(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	primary := []Record{
		{Seq: 20, Time: t0.Add(30 * time.Millisecond), ID: 0x703, DLC: 1, Data: [8]byte{3}, Interface: "can0"},
		{Seq: 21, Time: t0.Add(40 * time.Millisecond), ID: 0x704, DLC: 1, Data: [8]byte{4}, Interface: "can0"},
	}
	fallback := []Record{
		{Seq: 10, Time: t0.Add(10 * time.Millisecond), ID: 0x701, DLC: 1, Data: [8]byte{1}, Interface: "can0"},
		{Seq: 11, Time: t0.Add(20 * time.Millisecond), ID: 0x702, DLC: 1, Data: [8]byte{2}, Interface: "can0"},
	}

	merged := MergeRecords(primary, fallback, 3)
	if len(merged) != 3 {
		t.Fatalf("merged records = %d", len(merged))
	}
	for i, wantID := range []uint32{0x702, 0x703, 0x704} {
		if merged[i].ID != wantID {
			t.Fatalf("merged[%d] id = %03X want %03X; records=%#v", i, merged[i].ID, wantID, merged)
		}
	}
}

func TestMergeRecordsDoesNotCollapseDistinctRepeatedFrames(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	frame := Record{Seq: 1, Time: t0, ElapsedNanos: 1, ID: 0x701, DLC: 1, Data: [8]byte{0x05}, Interface: "can0"}
	repeated := frame
	repeated.Seq = 2
	repeated.Time = t0.Add(time.Millisecond)
	repeated.ElapsedNanos = 1_000_001

	merged := MergeRecords([]Record{frame}, []Record{repeated}, 0)
	if len(merged) != 2 {
		t.Fatalf("distinct repeated frames collapsed: %#v", merged)
	}
}
