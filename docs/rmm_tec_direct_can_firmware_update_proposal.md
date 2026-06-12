# MVP-Vorschlag: Meerstetter-Go gestützte RMM/TEC-Signalkonfiguration

Datum: 2026-06-12

Positionierung: Dies ist kein Wunsch nach einem großen neuen Meerstetter-Tooling-Framework. Meerstetter-Go kann die Offline-Konfiguration, den direkten USB-/Seriell-Betrieb eines einzelnen Geräts, Registry/Pattern-Export und den späteren Live-Abgleich bereits übernehmen. Die verbleibende Bitte an Meerstetter ist ein kleiner, stabil dokumentierter Firmware- und Konfigurations-MVP.

Ziel: Ein Setup soll auch ohne angeschlossenen CAN-Bus sinnvoll vorbereitet und geprüft werden können. Ohne CAN ist das eine konfigurierte Absicht mit Offline-Validierung oder ein einzelnes direkt per USB/Seriell angebundenes Gerät für Diagnose und Setup. Erst der Live-CAN-Betrieb beweist den hostfreien RMM->TEC Signalpfad nach Power-Cycle. Die erste konkrete Regelanwendung bleibt: RMM-1182 HR Pt100 Temperatur als Objekttemperatur-Regelsensor für einen TEC. Dieser Regelpfad darf im produktiven Betrieb nicht von Server-Software, USB-Verbindung oder Host-Polling-Loop abhängen.

## Kurzfassung

Die wichtigsten CANopen-Begriffe sind beim ersten Auftreten ausgeschrieben: `TPDO` steht für `Transmit Process Data Object` bzw. Sende-Prozessdatenobjekt, `RPDO` für `Receive Process Data Object` bzw. Empfangs-Prozessdatenobjekt, `NMT` für `Network Management`, `EMCY` für `Emergency Message`, `SDO` für `Service Data Object` und `PDO` für `Process Data Object`.

Der reduzierte MVP besteht aus vier Herstellerbeiträgen:

1. Dokumentierter Producer-Pfad: ein RMM oder anderes Producer-Gerät kann einen ausgewählten, typisierten Wert zyklisch als TPDO publizieren, inklusive Wertformat, Status/Güte, Event Timer und persistenter Speicherung.
2. Dokumentierter TEC-Consumer-Pfad: ein TEC kann ein RPDO als externe Objekttemperaturquelle verwenden, mit expliziter Quellenwahl, Empfangsfrist, Güteauswertung und sicherer Fehlerreaktion.
3. Maschinenlesbarer Status: Messwertgüte, Warnzustand, Sensorfehler und "nicht gültig" sind über MeCom und/oder CANopen eindeutig lesbar und passen zum LED-/Warnzustand.
4. Stabile Konfigurationsoberfläche: Meerstetter liefert die bestätigten Objektlisten, Speicherregeln und Sicherheitsgrenzen. Meerstetter-Go kann daraus Profile, Registry-Dateien, Import/Export, Plausibilisierung und Drift-Reports erzeugen.

Standard-CANopen-Objekte sollten bevorzugt werden, wo die Firmware sie sinnvoll unterstützt: Kommunikation und Mapping für TPDO/RPDO, NMT im Operational-Zustand, Heartbeat als zyklische Knoten-Lebensmeldung, EMCY, SDO für Konfiguration und PDO für Live-Daten. Der MVP muss aber nicht mit einer neuen CoSo-Oberfläche starten. Ein stabiler, dokumentierter Objekt- und Speicherpfad reicht, damit Meerstetter-Go die Setup-Ergonomie übernimmt.

## Betriebsmodi ohne vorausgesetzten CAN-Bus

Meerstetter-Go sollte denselben Aufbau in drei Modi behandeln:

1. **Offline / kein CAN angeschlossen.** Eine CAN-Signal-Registry beschreibt Rollen, COB-IDs, TPDO-/RPDO-Mappings, Source-Selects und erwartete Raten. Das ist eine validierte Absicht, kein Live-Nachweis. Pattern-Export und Import funktionieren ohne Hardware und können eine zweite Anlage vorbereiten.
2. **Einzelgerät direkt per USB/Seriell.** Ein einzelnes RMM oder TEC kann über MeCom gelesen und teilweise vorbereitet werden, z. B. `serial:COM5@57600` mit Adresse `0`, solange nur ein Gerät auf der Verbindung hängt. Für das RMM-1182 ist der read-only HR1/Pt100-Pfad bereits als Meerstetter-Go-Preset vorhanden.
3. **Live-CAN.** Erst dieser Modus beweist PDO-Verkehr, Node-ID-Kollisionen, Mapping-Drift, Producer-Frische und hostfreien Betrieb nach Power-Cycle.

Damit bleibt die Bedienung robust: Ohne CAN gibt es keine falschen roten Fehler, sondern einen klaren Status wie "nicht live geprüft". Mit CAN wird derselbe Registry-Stand gegen die Geräte gelesen und Signal für Signal als match, drift oder unknown bewertet.

## Was Meerstetter-Go bereits abdecken kann

Diese Punkte müssen nicht als großer Meerstetter-Featurewunsch formuliert werden:

- Registry/Pattern-Modell für kopierbare RMM-/TEC-Setups mit Rollen statt fest verdrahteter Seriennummern.
- Offline-Validierung von COB-ID-Kollisionen, fehlenden Rollen, Node-ID-Kollisionen, Mapping-Längen und Source-Selects.
- Import/Export eines Testbed-Patterns und Instanziierung mit neuen Node IDs.
- Direkte USB-/Seriell-Diagnose eines einzelnen RMM-1182 HR1 Pt100 über MeCom, inklusive read-only Preset.
- Gateway-/Web-Ansicht, die geplante Registry und Live-Abgleich trennt.
- Transportmodell für Serial, TCP und CAN, inklusive Fallback-/Redundanz-Metadaten.

Die Grenze ist bewusst: Meerstetter-Go darf eine Konfiguration beschreiben, prüfen und berichten. Für hostfreien RMM->TEC Regelbetrieb braucht es weiterhin einen von Meerstetter bestätigten Firmwarepfad, der Werte sicher publiziert, importiert, bindet, überwacht und speichert.

## Konkreter Beta-Test-Use-Case

Die geplante Anlage kombiniert voraussichtlich sechs RMM und vier TEC. Der Live-Betrieb nutzt ein lokales CAN-Netz; Vorbereitung und Dokumentation sollen aber auch offline oder mit einem einzelnen direkt angeschlossenen Gerät möglich sein:

- vier RMM als Temperaturmessstellen in der Thermalkammer;
- zwei RMM als Spannungs-/Current-Shunt-Readout;
- vier TEC als regelnde Consumer.

Nicht jede RMM-/TEC-Beziehung ist fest verdrahtet. Ein TEC-Kanal soll flexibel ein geeignetes RMM-Temperatursignal als Objekttemperatur abonnieren können; andere TECs können andere RMMs nutzen oder denselben Messwert nur diagnostisch sehen. Dasselbe Muster kann später auch für Führungswerte gelten, z. B. wenn mehrere TEC-Kanäle einem gemeinsamen Zieltemperaturwert folgen.

Der praktische Bedarf ist deshalb eine reproduzierbare Signal-Konfiguration:

- jedes lokale Netzwerk hat eindeutige Node IDs für alle RMM und TEC;
- ein Setup kann mehrfach kopiert werden, solange die Node-ID-Basis oder das Mapping eindeutig bleibt;
- die peer-to-peer Topologie ist eine Konfiguration, keine Firmware-Sonderverdrahtung;
- Meerstetter-Go kann Node IDs, Published Signals, Subscriptions und Control-Bindings exportieren, importieren und plausibilisieren; CoSo-Unterstützung wäre hilfreich, ist aber kein Blocker für den MVP.

Das verbessert unsere konkrete Anlage, verallgemeinert aber auch den RMM-Produktnutzen: Das RMM bleibt per USB/Seriell ein direkt nutzbares Messgerät für Host-Software und wird im CAN-Modus zusätzlich eine native Meerstetter-Signalquelle für andere Meerstetter-Geräte.

## Geräteunabhängige Zuordnung

Die CANopen Node ID sollte nur die lokale Transportadresse sein, nicht die fachliche Bedeutung des Geräts. Eindeutig wird ein Aufbau durch ein kleines lokales Profil:

- `Network Profile`: Name oder ID des lokalen Zusammenschlusses, z. B. `thermal-chamber-a`.
- `Node Block`: endliche lokale Adressbereiche pro Gerätetyp. CANopen erlaubt Node IDs `1..127`; praktisch sind 32er-Blöcke, z. B. `0x20-0x3F` für RMM und `0x40-0x5F` für TEC. Das entspricht bis zu 32 Geräten pro Typ in einem lokalen Zusammenschluss. Ungenutzte Lücken sind dabei bewusst erlaubt.
- `CANopen Identity`: Vendor, Product, Revision und Seriennummer aus `0x1018` als Plausibilitätsanker.
- `Signal ID`: stabiler fachlicher Name, z. B. `chamber_temp_01` oder `shunt_current_02`.
- `Signal Source`: Producer-Seriennummer, Producer-Kanal, Signaltyp, Einheit, Skalierung, Rate und Güteformat.
- `Control Binding`: Consumer-Seriennummer und Consumer-Kanal binden explizit auf eine `Signal ID`.

Damit können mehrere lokale CAN-Netze dieselben Node-ID-Blöcke verwenden, ohne dass ein TEC beim Anschluss an ein anderes CAN-Segment versehentlich einem falschen RMM folgt. Die eindeutige Zuordnung entsteht nicht aus der Node ID allein, sondern aus `Network Profile + CANopen Identity + Signal Source + Control Binding`. Ohne CAN ist dieses Profil nur geplante Absicht. Beim Boot oder nach Buswechsel gilt im Live-Betrieb: Die Firmware akzeptiert ein Control-Binding nur, wenn Node ID, CANopen Identity, Signaltyp, Kanalindex, Mapping-Version und Güte zum gespeicherten Profil passen. Andernfalls bleibt die importierte Regelquelle ungültig und geht in den sicheren Zustand.

Für unsere typische Anlage reicht dieses Modell komfortabel: sechs RMM und vier TEC belegen nur einen kleinen Teil der Blöcke. Die Anzahl bleibt trotzdem endlich und validierbar. Meerstetter-Go kann beim Commissioning erkennen, ob die aktuelle Busbelegung zum gespeicherten Profil passt, ob ein erwartetes Gerät fehlt, ob ein falsches Gerät auf einer erwarteten Node ID sitzt oder ob ein Signal zwar sichtbar, aber nicht für Control freigegeben ist. Ohne CAN bleibt derselbe Datensatz editierbar und prüfbar, nur die Live-Aussage bleibt "unknown".

## Generisches Modell

Die Funktion sollte als drei getrennte Ebenen formuliert werden:

1. `Published Signal`: Ein Gerät bietet einen typisierten Wert an, z. B. Temperatur, Spannung, Strom, Zieltemperatur, Kontrollwert oder Status.
2. `Signal Subscription`: Ein anderes Gerät importiert diesen Wert read-only und zeigt Wert, Alter, Güte und Quelle diagnostisch an.
3. `Control Source Binding`: Ein regelndes Gerät bindet ein geeignetes importiertes Signal explizit an eine Regelungsfunktion. Beobachten und Regeln bleiben getrennt.

Für den Firmware-MVP muss nicht das ganze Ökosystem umgesetzt werden. Es reicht, diese Rollen sauber zu benennen, damit die erste Firmwarefunktion nicht als Einzelfall "RMM schreibt TEC-Slot" endet.

## Aktuell verifizierte Fakten

Diese Punkte wurden lokal per CoSo-/EDS-Reverse-Engineering, direktem USB-MeCom-Readout und read-only CANopen-SDO-Probes bestätigt:

| Punkt | Befund |
| --- | --- |
| Physikalischer Bus | Alle vier TEC und drei RMM sind auf demselben CAN-Bus bei `1 Mbit/s`. |
| Node IDs | Keine Duplikate: RMM SN6 `0x37`, SN7 `0x38`, SN8 `0x39`; TEC SN75 `0x4B`, SN76 `0x4C`, SN81 `0x51`, SN84 `0x54`. Damit liegen die RMM in einem `0x20-0x3F`-Block und die TEC in einem `0x40-0x5F`-Block. |
| CANopen SDO | Alle sieben Geräte antworten auf CANopen SDO Identity/Error-Register Reads. Das ist nicht nur ein MeCom-over-CAN-Tunnel. |
| Produkt-IDs | RMM-1182 meldet Produkt `0x049E`; TEC-1089 meldet Produkt `0x0441`. |
| Fehlerregister | `0x1001:00 = 0x00` auf allen sieben Nodes. Das heißt nur: kein gesetztes CANopen-Error-Register-Bit. Es beweist nicht Operational-State, Heartbeat, PDO-Mapping oder Regelfähigkeit. |
| Prozessdatenobjekte | Standardnahe Kommunikations- und Mapping-Objekte für Process Data Objects (PDO) sind per Service Data Object (SDO) lesbar. |
| TPDO1 Default COB-ID | `0x180 + node_id` ist sichtbar: SN6 sendet aktuell auf `0x1B7`; SN7 wäre entsprechend `0x1B8`. |
| TPDO1 Mapping | Nur SN6 ist aktuell als TPDO-Publisher konfiguriert: SN6 `0x1A00:00 = 1`, Mapping `0x4000:01`, `32 bit`. SN7 und SN8 haben `0x1A00:00 = 0` und publizieren deshalb trotz eindeutiger Node IDs noch keinen Messwert. |
| SN7 Pt100-Wert | SN7 liefert über SDO einen plausiblen Pt100/Temperaturwert: HR1 Widerstand ca. `109.6 Ohm`, konvertierter Wert ca. `24.7 degC`. |
| Direkter USB-Fall | Ein RMM-1182 wurde über `COM5@57600` und MeCom-Adresse `0` direkt gelesen. HR1 Widerstand und VC1 Result lassen sich als read-only MeCom-Werte mit dem Preset `rmm-1182-hr1-pt100` prüfen. Das beweist Einzelgerät-Diagnose und Setup-Unterstützung, aber keinen hostfreien RMM->TEC Regelpfad. |
| Temperatur-Leserate | Die Firmware bietet für Temperaturwerte `1 Hz`, `10 Hz` und `90 Hz`; `10 Hz` ist der Default. |
| Aktueller Live-Traffic | Nur SN6 publiziert aktuell sichtbar zyklisch/eventgetrieben auf `0x1B7`. SN7/SN8 publizieren noch kein sichtbares TPDO. |
| Heartbeat aktuell | `0x1017` ist vorhanden, aber aktuell `0`; im passiven Capture waren keine `0x700 + node_id` Heartbeats sichtbar. |
| Heartbeat-Consumer/NMT-Startup | `0x1016` und `0x1F80` waren auf der aktuellen Firmware per Standard-SDO nicht verfügbar. Falls Meerstetter äquivalente Herstellerobjekte nutzt, brauchen wir die Dokumentation. |
| Rote LED | Das regelmäßige rote Blinken mit ca. `1 Hz` wurde von Meerstetter als Kennzeichen der aktuell nicht veröffentlichten Firmwareversion bestätigt. In neuer Firmware soll rot blinkend zusätzlich einen Warnzustand anzeigen, z. B. Messwert außerhalb des zulässigen Bereichs; optional gibt es dafür einen Dark Mode. |
| Digitale lokale Sensor-Interfaces | Meerstetter hat bestätigt, dass UART (Universal Asynchronous Receiver/Transmitter), SPI (Serial Peripheral Interface) und I2C (Inter-Integrated Circuit) hardwareseitig möglich sind; die Softwareunterstützung fehlt aktuell noch. |
| Buslast | Aktuell ca. 10 Frames/s und praktisch `0%` Buslast bei `1 Mbit/s`. Mehr zyklischer Prozessdatenverkehr wäre technisch gut vertretbar. |

Nicht bestätigt ist bisher:

- ob Producer-TPDO-Mapping schreibbar und persistent speicherbar ist;
- welcher sichere Konfigurationspfad SN7/SN8 von "Messwert per SDO sichtbar" zu "Messwert per TPDO publiziert" bringt;
- ob ein Producer einen Messwert zyklisch und nicht nur bei Änderung publizieren kann;
- ob die vorhandene Temperatur-Leserate `1 Hz` / `10 Hz` / `90 Hz` direkt als TPDO-Periode nutzbar ist oder ob dafür ein eigener PDO-Event-Timer nötig ist;
- ob ein Consumer ein fremdes RPDO als diagnostisch sichtbares importiertes Signal behandeln kann;
- ob ein TEC oder anderes regelndes Gerät ein importiertes Signal intern als Regelsensor- oder Führungswertquelle routen kann;
- ob ein regelndes Consumer-Gerät für importierte Signale eine Empfangsfrist, Heartbeat-/NMT-Überwachung und sicheren Fehlerzustand unterstützt;
- welche Status-/Güteobjekte für RMM-Messwerte und später andere Published Signals von Meerstetter bevorzugt werden;
- ob Meerstetter einen USB-/Seriell-only Modus nur als Setup-/Diagnosepfad sieht oder eine spätere hostgebundene Router-Funktion mit klarer Latenz-, Zeitstempel- und Timeout-Semantik unterstützen will.

## Firmware-MVP für Live-CAN

### Producer-Seite

Ein Producer-Gerät soll für einen konfigurierten lokalen Wert ein zyklisches TPDO anbieten. Der erste Producer ist ein RMM-1182 mit HR1 Pt100. Für den MVP reicht ein dokumentierter, sicher speicherbarer Pfad für diesen Fall; die Modellierung sollte aber nicht verhindern, dass andere RMM-Messwerte später denselben Mechanismus nutzen:

- Producer-Identität: Produkt, Seriennummer, CANopen Node ID.
- Lokale Quelle: Eingang/Kanal/Signalinstanz, im Testfall RMM SN7 HR1 Pt100.
- Signal-Identität: stabiler Name, Einheit, Skalierung, Kanalindex und Mapping-Version, damit ein Consumer nicht nur auf eine rohe COB-ID vertraut.
- TPDO COB-ID: bevorzugt Standard `0x180 + node_id`; im Testfall SN7 `0x1B8`.
- Payload: dokumentierter Temperaturwert plus Güte/Status.
- Zyklus: `10 Hz` als Default der Minimalversion, passend zum Firmware-Default für Temperaturwerte. `1 Hz` eignet sich eher für Diagnose oder sehr langsame Regelstrecken; `90 Hz` ist die sinnvolle High-Rate-Option, falls Meerstetter sie für TEC-Regelung, Filterung und Rauschverhalten empfiehlt.
- Zwingend: ein Event Timer oder eine synchrone PDO-Strategie. Reines Change-of-Value ohne zyklisches Lebenszeichen ist für einen Regelsensor ungeeignet.
- Fehler: Sensor-open/short/out-of-range/conversion fault müssen als Status und/oder EMCY sichtbar sein.
- Persistenz: Quelle, Mapping, COB-ID, Timer, Heartbeat und Enable-State müssen Power-Cycle überleben.

### Consumer-/Control-Seite

Ein Consumer-Gerät soll ein fremdes TPDO als importiertes Signal abonnieren und read-only diagnostisch sichtbar machen. Wenn das Consumer-Gerät eine Regelungsfunktion hat, soll es das Signal nur über ein explizites Control-Binding verwenden:

- Consumer-Identität: Produkt, Seriennummer, CANopen Node ID.
- Erwarteter Producer: Node ID, Produkt und Seriennummer als Commissioning-Check.
- Abonniertes TPDO: COB-ID und Profil-/Mapping-Version, im Testfall `0x1B8`.
- Importiertes Signal: Wert, Alter, Güte, Quelle, Receive Counter.
- Optionales Control-Binding: z. B. TEC-Objekttemperaturquelle für Kanal 1.
- Implementierungsvorschlag: interne Quellenwahl für `local sensor` / `host external object temperature` / `CANopen remote object temperature`; wenn möglich Wiederverwendung des bestehenden externen Objekttemperaturpfads für Filter, Limits und Fehlerreaktion.
- Fehlerreaktion: Output disable oder definierter sicherer Hold-State. Automatischer Fallback auf lokalen Sensor nur explizit und mit bumpless transfer/ramp limit, nicht als Default.
- Persistenz: Import, Frist, Güteauswertung, Quellenwahl und Kanalbindung müssen Power-Cycle überleben.

### Liveness und Güte

Das ist ein Gate für jede Regelanwendung:

- Importierter Regelsensor ist beim Boot ungültig, bis ein frischer gültiger Wert empfangen wurde.
- Max-Age/Empfangsfrist ist konfigurierbar, z. B. `3x` bis `10x` der TPDO-Periode.
- Heartbeat Producer/Consumer nach CiA-301 ist bevorzugt: RMM `0x1017`, TEC `0x1016`, falls in der Firmware verfügbar.
- Wenn `0x1016` nicht angeboten wird, sollte Meerstetter eine gleichwertige Consumer-seitige Producer-Liveness-Überwachung für importierte Regelsignale bereitstellen.
- NMT nicht Operational, Heartbeat-Verlust, RPDO-Deadline, ungültige Güte, unplausibler Sprung oder Wertebereichsverletzung machen die Quelle ungültig.
- Ein regelndes Consumer-Gerät darf nie stillschweigend mit einem eingefrorenen letzten Wert weiterregeln.

### Wire Contract

Die PDO-Definition sollte eindeutig sein, nicht implizit oder hostseitig geraten:

| Feld | Empfehlung |
| --- | --- |
| Wert | `float32` little-endian oder ein explizit dokumentierter skalierter Integer; für Temperatur vorzugsweise `degC`. |
| Status | vorhandenes RMM-Status-/Result-Flags-Objekt, CiA-404-Status falls vorhanden, oder ein dokumentiertes Meerstetter-Statuswort. |
| Einheit/Skalierung | als Teil der persistenten Konfiguration und Diagnose lesbar. |
| Source Check | Node ID und bei Commissioning bevorzugt Seriennummer/Produkt-ID. |
| Version | Mapping/Profile-Version, damit ein TEC falsche Payloads ablehnen kann. |

Für Classic CAN ist ein kleines PDO ausreichend. Beispiel: 4 Byte Wert + 2 Byte Status. Identität, Einheit, Quelle und Version müssen nicht in jedem Frame stehen, sondern können persistent konfiguriert und diagnostisch lesbar sein.

## Empfehlung zur CAN-Chattiness

Die aktuelle Buslast ist sehr niedrig. Für Regelsensoren ist etwas mehr deterministischer Traffic sinnvoller als zu wenig:

- `10 Hz` TPDO mit Event Timer als Default der Minimalversion, weil dies bereits der Temperatur-Firmware-Default ist.
- `1 Hz` nur für Diagnose oder sehr langsame thermische Strecken, nicht als Standard für einen TEC-Regelsensor.
- `90 Hz` ist bei wenigen Signalen und `1 Mbit/s` voraussichtlich busseitig unkritisch, sollte aber nur mit passender TEC-Filterung, Latenzbetrachtung und Rauschbewertung als High-Rate-Regeloption verwendet werden.
- Heartbeat z. B. `500 ms` bis `1000 ms`; Sensor-Max-Age separat und kürzer oder gleich der gewünschten Fehlerreaktionszeit.
- Kein dauerhaftes SDO-Polling für Live-Regelwerte.

Mehr zyklische Frames sind hier kein Problem an sich. Der Vorteil ist eine klare Frische-/Timeout-Semantik. Die Obergrenze sollte über Regelstabilität, nicht über Buslast, bestimmt werden.

## Lokaler CAN-Stand

Der lokale CAN-Teil ist in Bezug auf Busparameter und Node IDs bereits in einem sauberen Testprofil:

- `can0` läuft bei `1 Mbit/s`; alle sieben Meerstetter-Geräte antworten per CANopen SDO.
- RMM-Block: SN6 `0x37`, SN7 `0x38`, SN8 `0x39`.
- TEC-Block: SN75 `0x4B`, SN76 `0x4C`, SN81 `0x51`, SN84 `0x54`.
- SN6 ist der einzige aktuell ordnungsgemäß konfigurierte Live-Publisher: TPDO1 `0x1B7`, Mapping `0x4000:01`, `32 bit`, sichtbar mit ca. `10 Hz`.
- SN7 und SN8 haben eindeutige Node IDs und SDO-Kommunikation, aber kein TPDO1-Mapping. Das erklärt, warum sie auf CAN als Geräte sichtbar sind, aber noch keine Messwerte als Producer liefern.
- Die TEC haben aktuell keine RPDO-Control-Imports konfiguriert. Ein direkter RMM-zu-TEC-Regelpfad ist damit noch nicht aktiv.

Der nächste lokale Schritt ist nicht eine weitere Node-ID-Korrektur, sondern die reproduzierbare Producer-Konfiguration pro RMM-Messkanal: lokaler Eingang -> konvertierter Wert -> TPDO-Mapping -> zyklische Aussendung -> Persistenz. Meerstetter-Go kann die gewünschte Registry und den späteren Drift-Report vorbereiten. Meerstetter muss den sicheren Schreib-/Speicherpfad bestätigen, z. B. über CoSo/USB oder CANopen-SDO. Erst danach ist SN7 HR1 als Published Signal `chamber_temp_01` auf `0x1B8` ein sauberer Kandidat für den TEC-Import.

## Akzeptanztest für die Minimalversion

1. Ohne CAN kann Meerstetter-Go eine Registry oder ein Pattern laden, validieren, exportieren und als "nicht live geprüft" anzeigen.
2. Mit einem direkt per USB/Seriell angebundenen Einzelgerät kann Meerstetter-Go read-only Diagnosewerte lesen, z. B. RMM-1182 HR1/Pt100.
3. Im Live-CAN-Test laufen alle beteiligten Nodes bei `1 Mbit/s` mit eindeutigen Node IDs.
4. Die Node IDs liegen in einem lokalen, endlichen Profil, z. B. 32er-Blöcke pro Gerätetyp, und die Geräteidentität aus `0x1018` passt zum gespeicherten Profil.
5. Ein Producer publiziert ein typisiertes Signal per TPDO mit dokumentiertem Wert- und Statusformat.
6. Ein Consumer importiert dieses TPDO als read-only Signal und zeigt Wert, Alter, Güte und Quelle über normale Diagnose/MeCom an.
7. Ein regelndes Consumer-Gerät kann das importierte Signal explizit an eine Regelquelle binden.
8. Im ersten Test publiziert RMM SN7 HR1 Pt100 Temperatur auf TPDO `0x1B8`; TEC SN75 importiert dieses Signal als Objekttemperaturquelle für einen Kanal.
9. Nach Power-Cycle aller Geräte funktioniert die Verbindung ohne Host-Polling.
10. Stoppt das Producer-TPDO, fällt der Heartbeat aus, wird der Sensor abgezogen oder meldet der Producer ungültige Güte, geht eine gebundene Regelquelle innerhalb der konfigurierten Frist in den sicheren Fehlerzustand.
11. Meerstetter-Go kann dieselbe Konfiguration reproduzierbar exportieren, importieren und gegen Live-Geräte abgleichen; CoSo/XML-Unterstützung wäre eine spätere Komfortfunktion.

## MVP-Fragen an Meerstetter

1. Unterstützt die aktuelle Meerstetter-Firmware echte, persistent konfigurierbare CiA-301 PDO-Prozessdaten zwischen Geräten, oder ist CAN in Teilen nur MeCom-over-CAN?
2. Welcher konkrete Objekt- und Speicherpfad bringt RMM-1182 HR1/VC1 von "Messwert per SDO/MeCom sichtbar" zu "Messwert per TPDO zyklisch und persistent publiziert"?
3. Kann ein TEC ein RPDO heute als externe Objekttemperaturquelle verwenden? Falls nein: Ist der kleinste Firmwarepfad eine neue Quellenwahl, die ein CANopen-RPDO intern in den bestehenden externen Objekttemperaturpfad einspeist?
4. Welche Standard- oder Herstellerobjekte decken Event Timer, Heartbeat/Producer-Liveness, PDO-Deadline, EMCY, NMT-State, Empfangsalter und sichere Fehlerreaktion ab?
5. Wie sollen Messwertgüte, Sensorfehler, neuer Warnzustand und "nicht gültig" maschinenlesbar über MeCom und/oder CANopen abgebildet werden?
6. Welche Update-Rate empfiehlt Meerstetter für den ersten RMM->TEC Objekttemperaturpfad (`1 Hz`, `10 Hz`, `90 Hz`), inklusive Filterung, Resampling und Fehlerreaktionszeit?

## Produktfeedback und Pitch für Meerstetter-Go

Aus Beta-Tester-Sicht ist der stärkste Produktschritt nicht ein großes neues Tool, sondern ein sauberer Firmware- und Dokumentationsvertrag:

- Published measurement TPDO.
- Subscribed remote signal.
- Optional control-source binding, im ersten Schritt TEC remote object-temperature source.
- Persistenter Schreib-/Speicherpfad.
- Strikte Liveness-/Güteauswertung.
- Diagnose über MeCom/CANopen.

Meerstetter-Go kann daraus die nutzerfreundliche Seite bauen: Offline-Registry, Pattern für kopierte Anlagen, USB-Einzelgerät-Diagnose, Live-CAN-Drift-Report und später UI-gestützten Import/Export. Das reduziert den Meerstetter-Aufwand und macht trotzdem einen wiederholbaren Setup-Workflow möglich.

## Nicht Teil des MVP

Diese Punkte sollten nicht den ersten Firmware-Schritt blockieren:

1. Vollständige CoSo-Oberfläche für Published Signals, Subscriptions und Control-Bindings.
2. Allgemeines "Meerstetter Shared Signals" Framework für alle Gerätetypen.
3. Hostgebundene USB-/Seriell-Regelwert-Router. USB/Seriell bleibt im MVP Setup, Diagnose und Einzelgerät-Zugriff. Für Regelwerte wäre ein Router nur vertretbar, wenn Meerstetter dafür Latenz, Zeitstempel, Güte und Timeout-Semantik definiert.
4. UART, SPI und I2C als neue lokale Sensor-Interfaces. Sie sind später interessant, aber für den MVP nur zukünftige lokale Quellen.

## Lokale Evidenz

- `docs/rmm_1182_reverse_engineering.md`
- `docs/coso_compatibility_bridge.md`
- `docs/can_parameter_publishing_consuming_handbook.md`
- `docs/reference/can_signal_registry.example.json`
- `canmap/registry.go`
- `canmap/diff.go`
- `cmd/mecomgw/canmap.go`
- `cmd/mecomprobe/main.go`
- `mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`
- `mecom/catalogues/sources/tec_canopen_sdo_map.v631.json`
- Direkter USB-/Seriell-MeCom-Probe auf `COM5@57600` mit Adresse `0` am 2026-06-04.
- Read-only CANopen-SDO-Probes auf `can0` bei `1 Mbit/s` am 2026-06-08.
