# Featurevorschlag: Meerstetter CANopen-MVP fuer verteilte Mess- und Regelsignale

Datum: 2026-06-08

Positionierung: Dieser Vorschlag beschreibt einen kleinen, wiederverwendbaren CANopen-Baustein, mit dem Meerstetter-Geraete Mess- und Fuehrungssignale direkt untereinander austauschen koennen.

Ziel: Ein Meerstetter-Geraet bietet einen typisierten Mess- oder Fuehrungswert direkt ueber CAN/CANopen an. Ein anderes Meerstetter-Geraet abonniert diesen Wert persistent und kann ihn, falls sicher freigegeben, fuer Diagnose oder Regelung verwenden. Die erste konkrete Anwendung ist: RMM-1182 HR Pt100 Temperatur als Objekttemperatur-Regelsensor fuer einen TEC-1166. Die Regelstrecke darf nicht von Server-Software, USB-Verbindung oder Host-Polling-Loop abhaengen.

## Kurzfassung

Wir schlagen Meerstetter eine kleine, wiederverwendbare CANopen-Erweiterung vor:

1. Ein Producer-Geraet publiziert einen ausgewaehlten, typisierten Wert zyklisch als dokumentiertes TPDO.
2. Ein Consumer-Geraet kann dieses TPDO als sichtbares importiertes Signal abonnieren.
3. Ein regelndes Consumer-Geraet kann ein geeignetes importiertes Signal explizit als Regelquelle binden, z. B. als TEC-Objekttemperatur oder spaeter als Zielwertquelle.
4. Fuer TEC-Objekttemperatur fuehrt der TEC den importierten Wert idealerweise intern durch den bereits validierten Pfad fuer externe/Software-Objekttemperatur, aber mit expliziter Quellenwahl. Ein Host-geschriebener MeCom-Wert und ein CAN-sourcierter Wert duerfen nicht unkontrolliert denselben Slot beschreiben.
5. Das Consumer-Geraet ueberwacht Alter, Guete, NMT-/Heartbeat-Zustand und Empfangsfrist. Bei Timeout, ungueltigem Wert oder Sensordefekt geht eine gebundene Regelquelle in einen sicheren Fehlerzustand, statt den letzten gueltigen Wert endlos weiterzuverwenden.
6. Konfiguration, Import und Binding muessen persistent sein und nach Power-Cycle ohne Host weiterlaufen.

Standard-CANopen-Objekte sollten bevorzugt werden, wo immer die Firmware sie sinnvoll unterstuetzt: TPDO/RPDO-Kommunikation und Mapping, NMT Operational, Heartbeat, EMCY, SDO fuer Konfiguration und PDO fuer Live-Daten. Damit bleibt die Funktion technisch nachvollziehbar, diagnosefaehig und kompatibel mit bestehenden CANopen-Werkzeugen.

Der MVP bleibt bewusst klein: Producer-TPDO, Consumer-Import, optionales Control-Binding, Liveness/Guete, Persistenz. Die Laboranwendung RMM-1182 -> TEC-1089 ist der erste Validierungsfall, nicht die einzige Produktform.

## Konkreter Beta-Test-Use-Case

Die geplante Anlage kombiniert voraussichtlich sechs RMM und vier TEC auf einem lokalen CAN-Netz:

- vier RMM als Temperaturmessstellen in der Thermalkammer;
- zwei RMM als Spannungs-/Current-Shunt-Readout;
- vier TEC als regelnde Consumer.

Nicht jede RMM-/TEC-Beziehung ist fest verdrahtet. Ein TEC-Kanal soll flexibel ein geeignetes RMM-Temperatursignal als Objekttemperatur abonnieren koennen; andere TECs koennen andere RMMs nutzen oder denselben Messwert nur diagnostisch sehen. Dasselbe Muster kann spaeter auch fuer Fuehrungswerte gelten, z. B. wenn mehrere TEC-Kanaele einem gemeinsamen Zieltemperaturwert folgen.

Der praktische Bedarf ist deshalb eine reproduzierbare CAN-Node-ID- und Signal-Subscription-Konfiguration:

- jedes lokale Netzwerk hat eindeutige Node IDs fuer alle RMM und TEC;
- ein Setup kann mehrfach kopiert werden, solange die Node-ID-Basis oder das Mapping eindeutig bleibt;
- die peer-to-peer Topologie ist eine Konfiguration, keine Firmware-Sonderverdrahtung;
- CoSo/XML oder ein Meerstetter-Tool kann die Node IDs, Published Signals, Subscriptions und Control-Bindings exportieren, importieren und plausibilisieren.

Das verbessert unsere konkrete Anlage, verallgemeinert aber auch den RMM-Produktnutzen: Das RMM wird nicht nur ein Messgeraet fuer Host-Software, sondern eine native Meerstetter-Signalquelle fuer andere Meerstetter-Geraete.

## Generisches Modell

Die Funktion sollte als drei getrennte Ebenen formuliert werden:

1. `Published Signal`: Ein Geraet bietet einen typisierten Wert an, z. B. Temperatur, Spannung, Strom, Zieltemperatur, Kontrollwert oder Status.
2. `Signal Subscription`: Ein anderes Geraet importiert diesen Wert read-only und zeigt Wert, Alter, Guete und Quelle diagnostisch an.
3. `Control Source Binding`: Ein regelndes Geraet bindet ein geeignetes importiertes Signal explizit an eine Regelungsfunktion. Beobachten und Regeln bleiben getrennt.

Fuer den CANopen-MVP muss nicht das ganze Oekosystem umgesetzt werden. Es reicht, diese Rollen sauber zu benennen, damit die erste Firmwarefunktion nicht als Einzelfall "RMM schreibt TEC-Slot" endet.

## Aktuell verifizierte Fakten

Diese Punkte wurden lokal per CoSo-/EDS-Reverse-Engineering und read-only CANopen-SDO-Probes bestaetigt:

| Punkt | Befund |
| --- | --- |
| Physikalischer Bus | Alle vier TEC und drei RMM sind auf demselben CAN-Bus bei `1 Mbit/s`. |
| Node IDs | Keine Duplikate: RMM SN6 `0x37`, SN7 `0x38`, SN8 `0x39`; TEC SN75 `0x4B`, SN76 `0x4C`, SN81 `0x51`, SN84 `0x54`. |
| CANopen SDO | Alle sieben Geraete antworten auf CANopen SDO Identity/Error-Register Reads. Das ist nicht nur ein MeCom-over-CAN-Tunnel. |
| Produkt-IDs | RMM-1182 meldet Produkt `0x049E`; TEC-1089 meldet Produkt `0x0441`. |
| Fehlerregister | `0x1001:00 = 0x00` auf allen sieben Nodes. Das heisst nur: kein gesetztes CANopen-Error-Register-Bit. Es beweist nicht Operational-State, Heartbeat, PDO-Mapping oder Regelfaehigkeit. |
| TPDO/RPDO-Objekte | Standardnahe PDO-Kommunikations- und Mapping-Objekte sind per SDO lesbar. |
| TPDO1 Default COB-ID | `0x180 + node_id` ist sichtbar: SN6 sendet aktuell auf `0x1B7`; SN7 waere entsprechend `0x1B8`. |
| SN7 Pt100-Wert | SN7 liefert ueber SDO einen plausiblen Pt100/Temperaturwert: HR1 Widerstand ca. `109.6 Ohm`, konvertierter Wert ca. `24.7 degC`. |
| Aktueller Live-Traffic | Nur SN6 publiziert aktuell sichtbar zyklisch/eventgetrieben auf `0x1B7`. SN7/SN8 publizieren noch kein sichtbares TPDO. |
| Heartbeat aktuell | `0x1017` ist vorhanden, aber aktuell `0`; im passiven Capture waren keine `0x700 + node_id` Heartbeats sichtbar. |
| Heartbeat-Consumer/NMT-Startup | `0x1016` und `0x1F80` waren auf der aktuellen Firmware per Standard-SDO nicht verfuegbar. Falls Meerstetter aequivalente Herstellerobjekte nutzt, brauchen wir die Dokumentation. |
| Buslast | Aktuell ca. 10 Frames/s und praktisch `0%` Buslast bei `1 Mbit/s`. Mehr zyklischer Prozessdatenverkehr waere technisch gut vertretbar. |

Nicht bestaetigt ist bisher:

- ob Producer-TPDO-Mapping schreibbar und persistent speicherbar ist;
- ob ein Producer einen Messwert zyklisch und nicht nur bei Aenderung publizieren kann;
- ob ein Consumer ein fremdes RPDO als diagnostisch sichtbares importiertes Signal behandeln kann;
- ob ein TEC oder anderes regelndes Geraet ein importiertes Signal intern als Regelsensor- oder Fuehrungswertquelle routen kann;
- ob ein regelndes Consumer-Geraet fuer importierte Signale eine Empfangsfrist, Heartbeat-/NMT-Ueberwachung und sicheren Fehlerzustand unterstuetzt;
- welche Status-/Gueteobjekte fuer RMM-Messwerte und spaeter andere Published Signals von Meerstetter bevorzugt werden.

## CANopen-MVP

### Producer-Seite

Ein Producer-Geraet soll fuer einen konfigurierten lokalen Wert ein zyklisches TPDO anbieten. Der erste Producer ist ein RMM-1182 mit HR1 Pt100, aber dieselbe Logik sollte fuer andere RMM-Messwerte oder spaeter andere Meerstetter-Geraete gelten:

- Producer-Identitaet: Produkt, Seriennummer, CANopen Node ID.
- Lokale Quelle: Eingang/Kanal/Signalinstanz, im Testfall RMM SN7 HR1 Pt100.
- TPDO COB-ID: bevorzugt Standard `0x180 + node_id`; im Testfall SN7 `0x1B8`.
- Payload: dokumentierter Temperaturwert plus Guete/Status.
- Zyklus: initial `10 Hz`; optional `20 Hz` oder `50 Hz`, falls Meerstetter dies fuer die TEC-Regelung empfiehlt.
- Zwingend: ein Event Timer oder eine synchrone PDO-Strategie. Reines Change-of-Value ohne zyklisches Lebenszeichen ist fuer einen Regelsensor ungeeignet.
- Fehler: Sensor-open/short/out-of-range/conversion fault muessen als Status und/oder EMCY sichtbar sein.
- Persistenz: Quelle, Mapping, COB-ID, Timer, Heartbeat und Enable-State muessen Power-Cycle ueberleben.

### Consumer-/Control-Seite

Ein Consumer-Geraet soll ein fremdes TPDO als importiertes Signal abonnieren und read-only diagnostisch sichtbar machen. Wenn das Consumer-Geraet eine Regelungsfunktion hat, soll es das Signal nur ueber ein explizites Control-Binding verwenden:

- Consumer-Identitaet: Produkt, Seriennummer, CANopen Node ID.
- Erwarteter Producer: Node ID, Produkt, optional Seriennummer als Commissioning-Check.
- Abonniertes TPDO: COB-ID und Profil-/Mapping-Version, im Testfall `0x1B8`.
- Importiertes Signal: Wert, Alter, Guete, Quelle, Receive Counter.
- Optionales Control-Binding: z. B. TEC-Objekttemperaturquelle fuer Kanal 1.
- Implementierungsvorschlag: interne Quellenwahl fuer `local sensor` / `host external object temperature` / `CANopen remote object temperature`; wenn moeglich Wiederverwendung des bestehenden externen Objekttemperaturpfads fuer Filter, Limits und Fehlerreaktion.
- Fehlerreaktion: Output disable oder definierter sicherer Hold-State. Automatischer Fallback auf lokalen Sensor nur explizit und mit bumpless transfer/ramp limit, nicht als Default.
- Persistenz: Import, Frist, Gueteauswertung, Quellenwahl und Kanalbindung muessen Power-Cycle ueberleben.

### Liveness und Guete

Das ist ein Gate fuer jede Regelanwendung:

- Importierter Regelsensor ist beim Boot ungueltig, bis ein frischer gueltiger Wert empfangen wurde.
- Max-Age/Empfangsfrist ist konfigurierbar, z. B. `3x` bis `10x` der TPDO-Periode.
- Heartbeat Producer/Consumer nach CiA-301 ist bevorzugt: RMM `0x1017`, TEC `0x1016`, falls in der Firmware verfuegbar.
- Wenn `0x1016` nicht angeboten wird, sollte Meerstetter eine gleichwertige Consumer-seitige Producer-Liveness-Ueberwachung fuer importierte Regelsignale bereitstellen.
- NMT nicht Operational, Heartbeat-Verlust, RPDO-Deadline, ungueltige Guete, unplausibler Sprung oder Wertebereichsverletzung machen die Quelle ungueltig.
- Ein regelndes Consumer-Geraet darf nie stillschweigend mit einem eingefrorenen letzten Wert weiterregeln.

### Wire Contract

Die PDO-Definition sollte eindeutig sein, nicht implizit oder hostseitig geraten:

| Feld | Empfehlung |
| --- | --- |
| Wert | `float32` little-endian oder ein explizit dokumentierter skalierter Integer; fuer Temperatur vorzugsweise `degC`. |
| Status | vorhandenes RMM-Status-/Result-Flags-Objekt, CiA-404-Status falls vorhanden, oder ein dokumentiertes Meerstetter-Statuswort. |
| Einheit/Skalierung | als Teil der persistenten Konfiguration und Diagnose lesbar. |
| Source Check | Node ID und bei Commissioning bevorzugt Seriennummer/Produkt-ID. |
| Version | Mapping/Profile-Version, damit ein TEC falsche Payloads ablehnen kann. |

Fuer Classic CAN ist ein kleines PDO ausreichend. Beispiel: 4 Byte Wert + 2 Byte Status. Identitaet, Einheit, Quelle und Version muessen nicht in jedem Frame stehen, sondern koennen persistent konfiguriert und diagnostisch lesbar sein.

## Empfehlung zur CAN-Chattiness

Die aktuelle Buslast ist sehr niedrig. Fuer Regelsensoren ist etwas mehr deterministischer Traffic sinnvoller als zu wenig:

- Mindestens zyklisches `10 Hz` TPDO mit Event Timer.
- Wenn die TEC-Regelung davon profitiert, `20 Hz` als robuster Default pruefen.
- `50 Hz` ist fuer wenige Signale bei `1 Mbit/s` ebenfalls wahrscheinlich unkritisch, sollte aber gegen Messrauschen, TEC-Filterung und notwendige thermische Bandbreite abgewogen werden.
- Heartbeat z. B. `500 ms` bis `1000 ms`; Sensor-Max-Age separat und kuerzer oder gleich der gewuenschten Fehlerreaktionszeit.
- Kein dauerhaftes SDO-Polling fuer Live-Regelwerte.

Mehr zyklische Frames sind hier kein Problem an sich. Der Vorteil ist eine klare Frische-/Timeout-Semantik. Die Obergrenze sollte ueber Regelstabilitaet, nicht ueber Buslast, bestimmt werden.

## Akzeptanztest fuer den MVP

1. Alle beteiligten Nodes laufen bei `1 Mbit/s` mit eindeutigen Node IDs.
2. Ein Producer publiziert ein typisiertes Signal per TPDO mit dokumentiertem Wert- und Statusformat.
3. Ein Consumer importiert dieses TPDO als read-only Signal und zeigt Wert, Alter, Guete und Quelle ueber normale Diagnose/MeCom an.
4. Ein regelndes Consumer-Geraet kann das importierte Signal explizit an eine Regelquelle binden.
5. Im ersten Test publiziert RMM SN7 HR1 Pt100 Temperatur auf TPDO `0x1B8`; TEC SN75 importiert dieses Signal als Objekttemperaturquelle fuer einen Kanal.
6. Das gleiche Prinzip laesst sich auf ein Netzwerk mit sechs RMM und vier TEC anwenden, ohne andere Firmwarepfade pro Geraetekombination.
7. Die Kanal-Regelsensor-Auswahl zeigt die importierte Quelle eindeutig und persistent.
8. Nach Power-Cycle aller Geraete funktioniert die Verbindung ohne Host-Polling.
9. Stoppt das Producer-TPDO, faellt der Heartbeat aus, wird der Sensor abgezogen oder meldet der Producer ungueltige Guete, geht eine gebundene Regelquelle innerhalb der konfigurierten Frist in den sicheren Fehlerzustand.
10. CoSo/XML oder ein Meerstetter-Tool kann diese Konfiguration reproduzierbar exportieren und wiederherstellen.

## Zu klaerende Punkte

1. Unterstuetzt die aktuelle Meerstetter-Firmware echte, persistent konfigurierbare CiA-301 PDO-Prozessdaten zwischen Geraeten, oder ist CAN in Teilen nur MeCom-over-CAN?
2. Welche Objekte sollen als generische `Published Signals` fuer Messwert und Guete verwendet werden? Fuer RMM-1182 HR1 Pt100 sind Kandidaten aus der EDS konvertierte/surveillierte Resultate und Result-Flags.
3. Kann ein Producer-Geraet ein solches Signal heute zyklisch per TPDO senden, inklusive Event Timer und persistentem Mapping?
4. Kann ein Consumer-Geraet ein fremdes RPDO heute als read-only importiertes Signal mit Alter/Guete/Quelle darstellen?
5. Kann ein TEC heute ein importiertes Signal als Objekttemperaturquelle fuer einen Kanal verwenden?
6. Falls nein: Ist der kleinste TEC-Firmwarepfad eine neue Quellenwahl, die ein CANopen-RPDO intern in den bestehenden externen Objekttemperaturpfad einspeist?
7. Welche Standard-CANopen-Objekte sind fuer Heartbeat Producer/Consumer, EMCY, NMT-State und PDO-Deadline bereits verfuegbar?
8. Wenn `0x1016`/Consumer Heartbeat nicht verfuegbar ist: welche gleichwertige Consumer-seitige Liveness-Ueberwachung empfiehlt Meerstetter fuer importierte Regelsignale?
9. Wie wird die PDO-/Import-/Kanalbindung persistent gespeichert: CANopen `0x1010`, Meerstetter Flash/CoSo/XML oder ein gemischtes Modell?
10. Wie behandelt die TEC-Regelung einen externen Sensor mit 10/20 Hz Update-Rate? Gibt es Filterung, Resampling, Interpolation oder relevante D-Anteil-Daempfung?
11. Welche sichere Fehlerreaktion empfiehlt Meerstetter bei Timeout, ungueltiger Guete, out-of-range oder Sensordefekt?
12. Was bedeutet das ca. 1-Hz rote Blinken an den RMMs, wenn `0x1001:00 = 0x00` ist?
13. Kann CoSo kuenftig angebotene Signale anzeigen, Imports konfigurieren und Control-Bindings setzen, ohne manuelles Object-Dictionary-Editing?

## Produktfeedback

Aus Beta-Tester-Sicht ist der staerkste Produktschritt nicht ein grosses Framework, sondern ein klarer CANopen-MVP:

- Published measurement TPDO.
- Subscribed remote signal.
- Optional control-source binding, im ersten Schritt TEC remote object-temperature source.
- Standard-CANopen-Konfiguration und Persistenz.
- Strikte Liveness-/Gueteauswertung.
- Diagnose ueber MeCom/CoSo.

Das reicht fuer den konkreten Nutzen: ein Messgeraet wird zur echten externen Signalquelle fuer ein regelndes Geraet, ohne Server im Regelkreis. Gleichzeitig entsteht ein generisches Integrationsmuster fuer mehrere RMM, TEC und spaeter andere Meerstetter-Geraete in wiederholbaren Anlagen.

## Spekulativer Ausbau: Meerstetter Shared Signals

Wenn der MVP funktioniert, koennte daraus ein konsistentes Meerstetter-Oekosystemprimitive werden:

1. Jedes MeCom-Geraet kann typisierte `Published Signals` anbieten: Temperatur, Spannung, Strom, Zieltemperatur, Kontrollwert, Status.
2. Andere Geraete koennen solche Signale als `Signal Subscriptions` importieren.
3. Nur regelnde Geraete haben explizite `Control Source Bindings`, z. B. TEC-Kanal-Regelsensor oder TEC-Kanal-Zielwert folgt importiertem Signal.
4. Beobachten und Regeln bleiben getrennt. Ein importiertes Signal ist nicht automatisch eine Regelfreigabe.
5. Jedes Geraet kann lokale Werte plus konfigurierte Peer-Signale ueber seine normale MeCom-Schnittstelle read-only sichtbar machen, inklusive Alter, Guete und Quelle.
6. CAN/CANopen bleibt der bevorzugte Live-Control-Transport, weil PDOs echte Producer/Consumer-Kommunikation ohne Host ermoeglichen.
7. RS485/USB/UART/Ethernet-to-Serial bleiben prima fuer Commissioning, Diagnose und Sichtbarkeit. Fuer Regelwerte waeren sie nur geeignet, wenn Meerstetter einen eigenen Scheduler/Router mit gebundener Latenz, Zeitstempel, Guete und Timeout-Semantik definiert.
8. I2C/SPI am RMM sollten fuer diesen Zweck als lokale/periphere Schnittstellen behandelt werden, ausser Meerstetter definiert spaeter ein spezifisches Bridge-Profil.

Der Kundennutzen waere ein klarer, flottenweiter Workflow: ein Tool scannt Geraete, zeigt angebotene Signale, der Nutzer waehlt ein Signal aus, bindet es optional an eine TEC-Funktion, speichert die Konfiguration, und danach laufen die Geraete direkt miteinander. Mehrere Kopien einer Anlage koennen dasselbe Signalkonzept nutzen, auch wenn Node IDs oder konkrete RMM-/TEC-Paare pro Aufbau abweichen.

## Lokale Evidenz

- `docs/rmm_1182_reverse_engineering.md`
- `docs/coso_compatibility_bridge.md`
- `mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`
- `mecom/catalogues/sources/tec_canopen_sdo_map.v631.json`
- Read-only CANopen-SDO-Probes auf `can0` bei `1 Mbit/s` am 2026-06-08.


