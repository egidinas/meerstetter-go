# Featurevorschlag: Meerstetter CANopen-MVP für verteilte Mess- und Regelsignale

Datum: 2026-06-08

Positionierung: Dieser Vorschlag beschreibt einen kleinen, wiederverwendbaren CANopen-Baustein, mit dem Meerstetter-Geräte Mess- und Führungssignale direkt untereinander austauschen können.

Ziel: Ein Meerstetter-Gerät bietet einen typisierten Mess- oder Führungswert direkt über CAN/CANopen an. Ein anderes Meerstetter-Gerät abonniert diesen Wert persistent und kann ihn, falls sicher freigegeben, für Diagnose oder Regelung verwenden. Die erste konkrete Anwendung ist: RMM-1182 HR Pt100 Temperatur als Objekttemperatur-Regelsensor für einen TEC-1166. Die Regelstrecke darf nicht von Server-Software, USB-Verbindung oder Host-Polling-Loop abhängen.

## Kurzfassung

Wir schlagen Meerstetter eine kleine, wiederverwendbare CANopen-Erweiterung vor:

1. Ein Producer-Gerät publiziert einen ausgewählten, typisierten Wert zyklisch als dokumentiertes TPDO.
2. Ein Consumer-Gerät kann dieses TPDO als sichtbares importiertes Signal abonnieren.
3. Ein regelndes Consumer-Gerät kann ein geeignetes importiertes Signal explizit als Regelquelle binden, z. B. als TEC-Objekttemperatur oder später als Zielwertquelle.
4. Für TEC-Objekttemperatur führt der TEC den importierten Wert idealerweise intern durch den bereits validierten Pfad für externe/Software-Objekttemperatur, aber mit expliziter Quellenwahl. Ein Host-geschriebener MeCom-Wert und ein CAN-sourcierter Wert dürfen nicht unkontrolliert denselben Slot beschreiben.
5. Das Consumer-Gerät überwacht Alter, Güte, NMT-/Heartbeat-Zustand und Empfangsfrist. Bei Timeout, ungültigem Wert oder Sensordefekt geht eine gebundene Regelquelle in einen sicheren Fehlerzustand, statt den letzten gültigen Wert endlos weiterzuverwenden.
6. Konfiguration, Import und Binding müssen persistent sein und nach Power-Cycle ohne Host weiterlaufen.

Standard-CANopen-Objekte sollten bevorzugt werden, wo immer die Firmware sie sinnvoll unterstützt: TPDO/RPDO-Kommunikation und Mapping, NMT Operational, Heartbeat, EMCY, SDO für Konfiguration und PDO für Live-Daten. Damit bleibt die Funktion technisch nachvollziehbar, diagnosefähig und kompatibel mit bestehenden CANopen-Werkzeugen.

Der MVP bleibt bewusst klein: Producer-TPDO, Consumer-Import, optionales Control-Binding, Liveness/Güte, Persistenz. Die Laboranwendung RMM-1182 -> TEC-1089 ist der erste Validierungsfall, nicht die einzige Produktform.

## Konkreter Beta-Test-Use-Case

Die geplante Anlage kombiniert voraussichtlich sechs RMM und vier TEC auf einem lokalen CAN-Netz:

- vier RMM als Temperaturmessstellen in der Thermalkammer;
- zwei RMM als Spannungs-/Current-Shunt-Readout;
- vier TEC als regelnde Consumer.

Nicht jede RMM-/TEC-Beziehung ist fest verdrahtet. Ein TEC-Kanal soll flexibel ein geeignetes RMM-Temperatursignal als Objekttemperatur abonnieren können; andere TECs können andere RMMs nutzen oder denselben Messwert nur diagnostisch sehen. Dasselbe Muster kann später auch für Führungswerte gelten, z. B. wenn mehrere TEC-Kanäle einem gemeinsamen Zieltemperaturwert folgen.

Der praktische Bedarf ist deshalb eine reproduzierbare CAN-Node-ID- und Signal-Subscription-Konfiguration:

- jedes lokale Netzwerk hat eindeutige Node IDs für alle RMM und TEC;
- ein Setup kann mehrfach kopiert werden, solange die Node-ID-Basis oder das Mapping eindeutig bleibt;
- die peer-to-peer Topologie ist eine Konfiguration, keine Firmware-Sonderverdrahtung;
- CoSo/XML oder ein Meerstetter-Tool kann die Node IDs, Published Signals, Subscriptions und Control-Bindings exportieren, importieren und plausibilisieren.

Das verbessert unsere konkrete Anlage, verallgemeinert aber auch den RMM-Produktnutzen: Das RMM wird nicht nur ein Messgerät für Host-Software, sondern eine native Meerstetter-Signalquelle für andere Meerstetter-Geräte.

## Geräteunabhängige Zuordnung

Die CANopen Node ID sollte nur die lokale Transportadresse sein, nicht die fachliche Bedeutung des Geräts. Eindeutig wird ein Aufbau durch ein kleines lokales Profil:

- `Network Profile`: Name oder ID des lokalen Zusammenschlusses, z. B. `thermal-chamber-a`.
- `Node Block`: endliche lokale Adressbereiche pro Gerätetyp. CANopen erlaubt Node IDs `1..127`; praktisch sind 32er-Blöcke, z. B. `0x20-0x3F` für RMM und `0x40-0x5F` für TEC. Das entspricht bis zu 32 Geräten pro Typ in einem lokalen Zusammenschluss. Ungenutzte Lücken sind dabei bewusst erlaubt.
- `CANopen Identity`: Vendor, Product, Revision und Seriennummer aus `0x1018` als Plausibilitätsanker.
- `Signal ID`: stabiler fachlicher Name, z. B. `chamber_temp_01` oder `shunt_current_02`.
- `Signal Source`: Producer-Seriennummer, Producer-Kanal, Signaltyp, Einheit, Skalierung, Rate und Güteformat.
- `Control Binding`: Consumer-Seriennummer und Consumer-Kanal binden explizit auf eine `Signal ID`.

Damit können mehrere lokale CAN-Netze dieselben Node-ID-Blöcke verwenden, ohne dass ein TEC beim Anschluss an ein anderes CAN-Segment versehentlich einem falschen RMM folgt. Die eindeutige Zuordnung entsteht nicht aus der Node ID allein, sondern aus `Network Profile + CANopen Identity + Signal Source + Control Binding`. Beim Boot oder nach Buswechsel gilt: Die Firmware akzeptiert ein Control-Binding nur, wenn Node ID, CANopen Identity, Signaltyp, Kanalindex, Mapping-Version und Güte zum gespeicherten Profil passen. Andernfalls bleibt die importierte Regelquelle ungültig und geht in den sicheren Zustand.

Für unsere typische Anlage reicht dieses Modell komfortabel: sechs RMM und vier TEC belegen nur einen kleinen Teil der Blöcke. Die Anzahl bleibt trotzdem endlich und validierbar. Ein Tool kann beim Commissioning erkennen, ob die aktuelle Busbelegung zum gespeicherten Profil passt, ob ein erwartetes Gerät fehlt, ob ein falsches Gerät auf einer erwarteten Node ID sitzt oder ob ein Signal zwar sichtbar, aber nicht für Control freigegeben ist.

## Generisches Modell

Die Funktion sollte als drei getrennte Ebenen formuliert werden:

1. `Published Signal`: Ein Gerät bietet einen typisierten Wert an, z. B. Temperatur, Spannung, Strom, Zieltemperatur, Kontrollwert oder Status.
2. `Signal Subscription`: Ein anderes Gerät importiert diesen Wert read-only und zeigt Wert, Alter, Güte und Quelle diagnostisch an.
3. `Control Source Binding`: Ein regelndes Gerät bindet ein geeignetes importiertes Signal explizit an eine Regelungsfunktion. Beobachten und Regeln bleiben getrennt.

Für den CANopen-MVP muss nicht das ganze Ökosystem umgesetzt werden. Es reicht, diese Rollen sauber zu benennen, damit die erste Firmwarefunktion nicht als Einzelfall "RMM schreibt TEC-Slot" endet.

## Aktuell verifizierte Fakten

Diese Punkte wurden lokal per CoSo-/EDS-Reverse-Engineering und read-only CANopen-SDO-Probes bestätigt:

| Punkt | Befund |
| --- | --- |
| Physikalischer Bus | Alle vier TEC und drei RMM sind auf demselben CAN-Bus bei `1 Mbit/s`. |
| Node IDs | Keine Duplikate: RMM SN6 `0x37`, SN7 `0x38`, SN8 `0x39`; TEC SN75 `0x4B`, SN76 `0x4C`, SN81 `0x51`, SN84 `0x54`. Damit liegen die RMM in einem `0x20-0x3F`-Block und die TEC in einem `0x40-0x5F`-Block. |
| CANopen SDO | Alle sieben Geräte antworten auf CANopen SDO Identity/Error-Register Reads. Das ist nicht nur ein MeCom-over-CAN-Tunnel. |
| Produkt-IDs | RMM-1182 meldet Produkt `0x049E`; TEC-1089 meldet Produkt `0x0441`. |
| Fehlerregister | `0x1001:00 = 0x00` auf allen sieben Nodes. Das heißt nur: kein gesetztes CANopen-Error-Register-Bit. Es beweist nicht Operational-State, Heartbeat, PDO-Mapping oder Regelfähigkeit. |
| TPDO/RPDO-Objekte | Standardnahe PDO-Kommunikations- und Mapping-Objekte sind per SDO lesbar. |
| TPDO1 Default COB-ID | `0x180 + node_id` ist sichtbar: SN6 sendet aktuell auf `0x1B7`; SN7 wäre entsprechend `0x1B8`. |
| TPDO1 Mapping | Nur SN6 ist aktuell als TPDO-Publisher konfiguriert: SN6 `0x1A00:00 = 1`, Mapping `0x4000:01`, `32 bit`. SN7 und SN8 haben `0x1A00:00 = 0` und publizieren deshalb trotz eindeutiger Node IDs noch keinen Messwert. |
| SN7 Pt100-Wert | SN7 liefert über SDO einen plausiblen Pt100/Temperaturwert: HR1 Widerstand ca. `109.6 Ohm`, konvertierter Wert ca. `24.7 degC`. |
| Temperatur-Leserate | Die Firmware bietet für Temperaturwerte `1 Hz`, `10 Hz` und `90 Hz`; `10 Hz` ist der Default. |
| Aktueller Live-Traffic | Nur SN6 publiziert aktuell sichtbar zyklisch/eventgetrieben auf `0x1B7`. SN7/SN8 publizieren noch kein sichtbares TPDO. |
| Heartbeat aktuell | `0x1017` ist vorhanden, aber aktuell `0`; im passiven Capture waren keine `0x700 + node_id` Heartbeats sichtbar. |
| Heartbeat-Consumer/NMT-Startup | `0x1016` und `0x1F80` waren auf der aktuellen Firmware per Standard-SDO nicht verfügbar. Falls Meerstetter äquivalente Herstellerobjekte nutzt, brauchen wir die Dokumentation. |
| Buslast | Aktuell ca. 10 Frames/s und praktisch `0%` Buslast bei `1 Mbit/s`. Mehr zyklischer Prozessdatenverkehr wäre technisch gut vertretbar. |

Nicht bestätigt ist bisher:

- ob Producer-TPDO-Mapping schreibbar und persistent speicherbar ist;
- welcher sichere Konfigurationspfad SN7/SN8 von "Messwert per SDO sichtbar" zu "Messwert per TPDO publiziert" bringt;
- ob ein Producer einen Messwert zyklisch und nicht nur bei Änderung publizieren kann;
- ob die vorhandene Temperatur-Leserate `1 Hz` / `10 Hz` / `90 Hz` direkt als TPDO-Periode nutzbar ist oder ob dafür ein eigener PDO-Event-Timer nötig ist;
- ob ein Consumer ein fremdes RPDO als diagnostisch sichtbares importiertes Signal behandeln kann;
- ob ein TEC oder anderes regelndes Gerät ein importiertes Signal intern als Regelsensor- oder Führungswertquelle routen kann;
- ob ein regelndes Consumer-Gerät für importierte Signale eine Empfangsfrist, Heartbeat-/NMT-Überwachung und sicheren Fehlerzustand unterstützt;
- welche Status-/Güteobjekte für RMM-Messwerte und später andere Published Signals von Meerstetter bevorzugt werden.

## CANopen-MVP

### Producer-Seite

Ein Producer-Gerät soll für einen konfigurierten lokalen Wert ein zyklisches TPDO anbieten. Der erste Producer ist ein RMM-1182 mit HR1 Pt100, aber dieselbe Logik sollte für andere RMM-Messwerte oder später andere Meerstetter-Geräte gelten:

- Producer-Identität: Produkt, Seriennummer, CANopen Node ID.
- Lokale Quelle: Eingang/Kanal/Signalinstanz, im Testfall RMM SN7 HR1 Pt100.
- Signal-Identität: stabiler Name, Einheit, Skalierung, Kanalindex und Mapping-Version, damit ein Consumer nicht nur auf eine rohe COB-ID vertraut.
- TPDO COB-ID: bevorzugt Standard `0x180 + node_id`; im Testfall SN7 `0x1B8`.
- Payload: dokumentierter Temperaturwert plus Güte/Status.
- Zyklus: `10 Hz` als MVP-Default, passend zum Firmware-Default für Temperaturwerte. `1 Hz` eignet sich eher für Diagnose oder sehr langsame Regelstrecken; `90 Hz` ist die sinnvolle High-Rate-Option, falls Meerstetter sie für TEC-Regelung, Filterung und Rauschverhalten empfiehlt.
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

- `10 Hz` TPDO mit Event Timer als MVP-Default, weil dies bereits der Temperatur-Firmware-Default ist.
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

Der nächste lokale Schritt ist nicht eine weitere Node-ID-Korrektur, sondern die reproduzierbare Producer-Konfiguration pro RMM-Messkanal: lokaler Eingang -> konvertierter Wert -> TPDO-Mapping -> zyklische Aussendung -> Persistenz. Diese Sequenz sollte über CoSo/USB oder einen von Meerstetter bestätigten CANopen-SDO-Schreibpfad erfolgen. Erst danach ist SN7 HR1 als Published Signal `chamber_temp_01` auf `0x1B8` ein sauberer Kandidat für den TEC-Import.

## Akzeptanztest für den MVP

1. Alle beteiligten Nodes laufen bei `1 Mbit/s` mit eindeutigen Node IDs.
2. Die Node IDs liegen in einem lokalen, endlichen Profil, z. B. 32er-Blöcke pro Gerätetyp, und die Geräteidentität aus `0x1018` passt zum gespeicherten Profil.
3. Ein Producer publiziert ein typisiertes Signal per TPDO mit dokumentiertem Wert- und Statusformat.
4. Ein Consumer importiert dieses TPDO als read-only Signal und zeigt Wert, Alter, Güte und Quelle über normale Diagnose/MeCom an.
5. Ein regelndes Consumer-Gerät kann das importierte Signal explizit an eine Regelquelle binden.
6. Im ersten Test publiziert RMM SN7 HR1 Pt100 Temperatur auf TPDO `0x1B8`; TEC SN75 importiert dieses Signal als Objekttemperaturquelle für einen Kanal.
7. Das gleiche Prinzip lässt sich auf ein Netzwerk mit sechs RMM und vier TEC anwenden, ohne andere Firmwarepfade pro Gerätekombination.
8. Die Kanal-Regelsensor-Auswahl zeigt die importierte Quelle eindeutig und persistent.
9. Nach Power-Cycle aller Geräte funktioniert die Verbindung ohne Host-Polling.
10. Stoppt das Producer-TPDO, fällt der Heartbeat aus, wird der Sensor abgezogen oder meldet der Producer ungültige Güte, geht eine gebundene Regelquelle innerhalb der konfigurierten Frist in den sicheren Fehlerzustand.
11. CoSo/XML oder ein Meerstetter-Tool kann diese Konfiguration reproduzierbar exportieren und wiederherstellen.

## Zu klärende Punkte

1. Unterstützt die aktuelle Meerstetter-Firmware echte, persistent konfigurierbare CiA-301 PDO-Prozessdaten zwischen Geräten, oder ist CAN in Teilen nur MeCom-over-CAN?
2. Welche Objekte sollen als generische `Published Signals` für Messwert und Güte verwendet werden? Für RMM-1182 HR1 Pt100 sind Kandidaten aus der EDS konvertierte/surveillierte Resultate und Result-Flags.
3. Kann ein Producer-Gerät ein solches Signal heute zyklisch per TPDO senden, inklusive Event Timer und persistentem Mapping?
4. Kann ein Consumer-Gerät ein fremdes RPDO heute als read-only importiertes Signal mit Alter/Güte/Quelle darstellen?
5. Kann ein TEC heute ein importiertes Signal als Objekttemperaturquelle für einen Kanal verwenden?
6. Falls nein: Ist der kleinste TEC-Firmwarepfad eine neue Quellenwahl, die ein CANopen-RPDO intern in den bestehenden externen Objekttemperaturpfad einspeist?
7. Welche Standard-CANopen-Objekte sind für Heartbeat Producer/Consumer, EMCY, NMT-State und PDO-Deadline bereits verfügbar?
8. Wenn `0x1016`/Consumer Heartbeat nicht verfügbar ist: welche gleichwertige Consumer-seitige Liveness-Überwachung empfiehlt Meerstetter für importierte Regelsignale?
9. Wie wird die PDO-/Import-/Kanalbindung persistent gespeichert: CANopen `0x1010`, Meerstetter Flash/CoSo/XML oder ein gemischtes Modell?
10. Wie behandelt die TEC-Regelung einen externen Sensor mit `1 Hz`, `10 Hz` oder `90 Hz` Update-Rate? Gibt es Filterung, Resampling, Interpolation oder relevante D-Anteil-Dämpfung, und welche Rate empfiehlt Meerstetter als Control-Default?
11. Welche sichere Fehlerreaktion empfiehlt Meerstetter bei Timeout, ungültiger Güte, out-of-range oder Sensordefekt?
12. Was bedeutet das ca. 1-Hz rote Blinken an den RMMs, wenn `0x1001:00 = 0x00` ist?
13. Kann CoSo künftig angebotene Signale anzeigen, Imports konfigurieren und Control-Bindings setzen, ohne manuelles Object-Dictionary-Editing?

## Produktfeedback

Aus Beta-Tester-Sicht ist der stärkste Produktschritt nicht ein großes Framework, sondern ein klarer CANopen-MVP:

- Published measurement TPDO.
- Subscribed remote signal.
- Optional control-source binding, im ersten Schritt TEC remote object-temperature source.
- Standard-CANopen-Konfiguration und Persistenz.
- Strikte Liveness-/Güteauswertung.
- Diagnose über MeCom/CoSo.

Das reicht für den konkreten Nutzen: ein Messgerät wird zur echten externen Signalquelle für ein regelndes Gerät, ohne Server im Regelkreis. Gleichzeitig entsteht ein generisches Integrationsmuster für mehrere RMM, TEC und später andere Meerstetter-Geräte in wiederholbaren Anlagen.

## Spekulativer Ausbau: Meerstetter Shared Signals

Wenn der MVP funktioniert, könnte daraus ein konsistentes Meerstetter-Ökosystemprimitive werden:

1. Jedes MeCom-Gerät kann typisierte `Published Signals` anbieten: Temperatur, Spannung, Strom, Zieltemperatur, Kontrollwert, Status.
2. Andere Geräte können solche Signale als `Signal Subscriptions` importieren.
3. Nur regelnde Geräte haben explizite `Control Source Bindings`, z. B. TEC-Kanal-Regelsensor oder TEC-Kanal-Zielwert folgt importiertem Signal.
4. Beobachten und Regeln bleiben getrennt. Ein importiertes Signal ist nicht automatisch eine Regelfreigabe.
5. Jedes Gerät kann lokale Werte plus konfigurierte Peer-Signale über seine normale MeCom-Schnittstelle read-only sichtbar machen, inklusive Alter, Güte und Quelle.
6. CAN/CANopen bleibt der bevorzugte Live-Control-Transport, weil PDOs echte Producer/Consumer-Kommunikation ohne Host ermöglichen.
7. RS485/USB/UART/Ethernet-to-Serial bleiben prima für Commissioning, Diagnose und Sichtbarkeit. Für Regelwerte wären sie nur geeignet, wenn Meerstetter einen eigenen Scheduler/Router mit gebundener Latenz, Zeitstempel, Güte und Timeout-Semantik definiert.
8. I2C/SPI am RMM sollten für diesen Zweck als lokale/periphere Schnittstellen behandelt werden, außer Meerstetter definiert später ein spezifisches Bridge-Profil.

Der Kundennutzen wäre ein klarer, flottenweiter Workflow: ein Tool scannt Geräte, zeigt angebotene Signale, der Nutzer wählt ein Signal aus, bindet es optional an eine TEC-Funktion, speichert die Konfiguration, und danach laufen die Geräte direkt miteinander. Mehrere Kopien einer Anlage können dasselbe Signalkonzept nutzen, auch wenn Node IDs oder konkrete RMM-/TEC-Paare pro Aufbau abweichen.

## Lokale Evidenz

- `docs/rmm_1182_reverse_engineering.md`
- `docs/coso_compatibility_bridge.md`
- `mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`
- `mecom/catalogues/sources/tec_canopen_sdo_map.v631.json`
- Read-only CANopen-SDO-Probes auf `can0` bei `1 Mbit/s` am 2026-06-08.
