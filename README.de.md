# LAVRay für Android

[Русский](README.md) · [English](README.en.md) · [中文](README.zh.md) · [Español](README.es.md) · **Deutsch**

*Dies ist eine Übersetzung der russischen README. Die Versionshinweise (Release Notes) sind auf Russisch.*

**LAVRay** für Android ist ein VPN-Client fürs Smartphone. Es ist eine native App in Kotlin und Jetpack Compose auf dem gleichen Go-Core wie [LAVRay für Windows](https://github.com/anatolijlaptev1991-ctrl/lavray-windows): gleiche Namen, gleiche Routing-Sets, gleiche Bildschirme und gleiche Farben. Die App verbindet sich mit deinen Servern und Abos und sendet dem Abo-Anbieter standardmäßig keine Daten über dein Gerät.

<p align="center">
  <img src="docs/screenshots/connection-dark.png" width="32%" alt="Bildschirm „Verbindung“, dunkles Design">
  <img src="docs/screenshots/servers-dark.png" width="32%" alt="Bildschirm „Server“, dunkles Design">
  <img src="docs/screenshots/rules-dark.png" width="32%" alt="Bildschirm „Regeln“, dunkles Design">
</p>
<p align="center">
  <img src="docs/screenshots/diagnostics-dark.png" width="32%" alt="Bildschirm „Diagnose“, dunkles Design">
  <img src="docs/screenshots/servers-light.png" width="32%" alt="Bildschirm „Server“, helles Design">
  <img src="docs/screenshots/settings-light.png" width="32%" alt="Bildschirm „Einstellungen“, helles Design">
</p>

*Die Screenshots stammen von einem Emulator, die Oberfläche ist auf Englisch.*

> **Stand: Version 0.9.4. Auf einem Android-16-Emulator (x86_64) geprüft; der Eigentümer testet sie auf einem echten Smartphone.** Diese Version folgt auf ein vollständiges Audit: [docs/AUDIT-2026-11.md](docs/AUDIT-2026-11.md) (Russisch). Eine Checkliste für den Test auf dem Smartphone steht in [docs/PHONE-CHECKLIST.md](docs/PHONE-CHECKLIST.md) (Russisch).

## Funktionen

- **Server und Gruppen:** Suche, Favoriten, Filter „Schnelle“ und „Unerreichbar“, Sortierung, Verschieben zwischen Gruppen, Kopien und ein QR-Code für jeden Server.
- **Abos** mit zeitgesteuerter Aktualisierung und Rückkehr zum letzten funktionierenden Stand. Ein Abo-Link, auch ein verschlüsselter, legt beim Import das Abo an und lädt sofort die Server; die Meldung nennt die tatsächliche Zahl der Server.
- **Import:** Link, Zwischenablage, QR-Code (Kamera oder Bild) und Datei. Links wie `vless://`, `vmess://`, `trojan://`, `ss://`, `hy2://`, `tuic://` lassen sich aus einer anderen App in LAVRay öffnen oder über „Teilen“ senden. Es wird erst importiert, wenn du bestätigst.
- **Ehrlicher Verbindungsstatus:** Nach dem Verbinden prüft der Core, ob der Datenverkehr wirklich über den Server läuft, und der Bildschirm zeigt das Ergebnis. „Kanal ist geschützt“ steht nur, wenn kein Problem gefunden wurde.
- **Änderungen, die einen Neustart brauchen** (Routing-Set, DNS, MTU, App-Liste), sind mit „gilt nach dem Neustart“ markiert, und die App bietet den Neustart an.
- **Latenztest** für alle Server und ein URL-Test.
- **Routing-Sets:** „Global“, „Alles durch den Proxy“, „Alles direkt“, **LAV Preset RU Linux** (standardmäßig aktiv: LAV Preset RU ohne die Regeln für Windows-Programme; russische Seiten und Spiele direkt, alles andere über den Proxy), LAV Preset RU (Revision 3) und LAV Preset CN. Eigene Sets, Import und Export sowie eine Prüfung, wohin eine Adresse geroutet wird.
- **DNS**, **Verbindungen**, **Protokoll**, **Statistik** und der Bildschirm „Warum funktioniert es nicht?“.
- **Tunnel pro App:** alle Apps, nur ausgewählte oder alle außer ausgewählten.
- **Automatisches Verbinden nach dem Neustart** mit dem zuletzt genutzten Server und Unterstützung für das systemweite Always-on-VPN. Der Tunnel kommt nach einem App-Update und nach dem Beenden des Dienstes durch das System zurück.
- **Kein direkter Datenverkehr beim Serverwechsel:** Beim Serverwechsel, bei „Neu verbinden“ und beim Neustart des Core bleibt die VPN-Schnittstelle dieselbe, und der Datenverkehr der Apps wird höchstens 30 Sekunden lang angehalten, nicht direkt gesendet. Ausnahme: Ändern sich die Parameter der Schnittstelle selbst (MTU, IPv6, Tunnel-DNS, App-Liste), legt Android eine neue Schnittstelle an, und für Sekundenbruchteile ist eine Lücke möglich.
- **Sicherung** in eine verschlüsselte Datei über die Dateiauswahl des Systems.
- **Fünf Oberflächensprachen** (Русский, English, 中文, Español, Deutsch), helles und dunkles Design, mehrere Akzentfarben.

Die Protokolle entsprechen der Desktop-Version: VLESS (auch REALITY), VMess, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS, HTTP.

---

## Herunterladen

Die APKs liegen auf der Seite [Releases](https://github.com/anatolijlaptev1991-ctrl/lavray-android/releases); die neueste Version ist [v0.9.4](https://github.com/anatolijlaptev1991-ctrl/lavray-android/releases/tag/v0.9.4). Für jede CPU-Architektur gibt es eine eigene Datei:

| Datei | Wofür | Dateigröße |
|---|---|---|
| `LAVRay-<Version>-arm64-v8a.apk` | fast alle aktuellen Smartphones und Tablets | 16,21 MB |
| `LAVRay-<Version>-armeabi-v7a.apk` | alte 32-Bit-Geräte | 16,68 MB |
| `LAVRay-<Version>-x86_64.apk` | Emulatoren und Chromebooks | 17,27 MB |
| `SHA256SUMS` | Prüfsummen | — |

Wenn du unsicher bist, nimm `arm64-v8a`. Die Größen stammen aus [docs/MEASUREMENTS.md](docs/MEASUREMENTS.md) (Russisch).

**Datei prüfen.** Lade die APK und `SHA256SUMS` in denselben Ordner:

```bash
sha256sum -c --ignore-missing SHA256SUMS      # Linux, macOS (unter macOS: shasum -a 256 -c)
```

```powershell
Get-FileHash .\LAVRay-<Version>-arm64-v8a.apk -Algorithm SHA256   # Windows: mit der Zeile in SHA256SUMS vergleichen
```

**Signatur.** Release-APKs werden mit dem Schlüssel des Eigentümers signiert, wie in [docs/SIGNING.md](docs/SIGNING.md) (Russisch) beschrieben. Ein Update lässt sich nur über eine vorhandene Installation einspielen, wenn es dieselbe Signatur trägt.

Zertifikat `CN=LAVRay, O=LAVRay`, SHA-256-Fingerabdruck: `fe058a98b9abb7ba8c853c0ae39c893aeeffa6892eb5a243e22718c2ac58fbb3`. Prüfen mit `apksigner verify --print-certs <Datei>.apk`.

**Voraussetzungen:** Android 8.0 oder neuer (minSdk 26), ARM-Prozessor (64 oder 32 Bit) oder x86_64.

---

## Schnellstart

1. **APK installieren.** Erlaube die Installation aus der gewählten Quelle (Browser, Dateien), öffne die Datei und bestätige.
2. **Server hinzufügen.** „Verbindung“ → „Server hinzufügen“ oder „Server“ → Symbol „+“. Füge einen Server- oder Abo-Link ein, scanne einen QR-Code mit der Kamera, wähle ein Bild mit QR-Code oder eine Datei. Vor dem Import siehst du, was hinzugefügt wird.
3. **Verbinden.** „Verbindung“ → „Verbinden“. Ab Android 13 fragt die App zuerst nach der Benachrichtigungsberechtigung (das VPN läuft als Vordergrunddienst mit einer Benachrichtigung), danach erscheint der VPN-Zustimmungsdialog: Bestätige beides. Nach dem Verbinden zeigt der Bildschirm Land und Adresse des Ausgangs, die Leak-Prüfung und die Geschwindigkeit.
4. **Routen prüfen.** LAV Preset RU Linux ist bereits aktiv: Russische Seiten öffnen direkt, alles andere läuft über den Server. Ein anderes Set wählst du unter „Regeln“.
5. **Automatisch verbinden.** „Einstellungen“ → „Start“ → „Beim Start des Telefons verbinden“ (standardmäßig aus). Details und Grenzen stehen weiter unten.

---

## Bedienung

### Server und Abos

- „Alle testen“ misst die Latenz. Im Ansichtsmenü gibt es den „URL-Test“: Über jeden Server wird eine echte Seite geöffnet. Die aktuelle Verbindung bleibt während der Tests bestehen.
- Servermenü: „Verbinden“, Favorit, „Link kopieren“, „Link teilen“, „QR-Code anzeigen“ (der Link enthält Schlüssel, die App warnt dich), „Duplizieren“, „In Gruppe verschieben…“, Reihenfolge, Löschen. Mit langem Tippen wählst du mehrere Server aus.
- Abos: „Abos“ → Link hinzufügen, dann „Aktualisieren“. Ein fehlgeschlagenes Update löscht die alten Server nicht, und du kannst es „Zurücksetzen“.
- XHTTP-Links werden importiert, sind aber mit „vom Core nicht unterstützt“ markiert: Eine Verbindung darüber ist nicht möglich.

### Regeln und DNS

- Eingebaute Sets werden nie verändert: „Kopie anpassen“ legt deine eigene Kopie an, „Auf Original zurücksetzen“ stellt sie wieder her.
- „Route prüfen“ zeigt, wohin eine Domain oder IP mit dem aktiven Set geroutet wird.
- DNS: „Auto“ (passend zum aktiven Set), lokal, DoH, DoT und durch den Tunnel, eigene Server, FakeIP.

### Diagnose

- **Prüfungen** („Warum funktioniert es nicht?“): Core, Server, Tunnel, DNS, Uhrzeit, Abo.
- **Verbindungen:** jede Verbindung mit Adresse, Ausgang, Regel und Datenmenge, mit Suche, Pause und Schließen.
- **Protokoll:** „Kopieren“ und „Teilen“ liefern das Protokoll mit bereits verborgenen Passwörtern, UUIDs, Tokens und Abo-Adressen. Du kannst es einer Fehlermeldung beilegen.
- **Statistik:** Datenverkehr der Sitzung und insgesamt, nach Gruppe und nach Route.

### Tunnel pro App

„Einstellungen“ → „Tunnel“ → „Apps im Tunnel“: alle Apps, nur ausgewählte oder alle außer ausgewählten. LAVRay selbst bleibt immer außerhalb des Tunnels. Eine Änderung gilt nach dem erneuten Verbinden: Die App bietet „Jetzt neu verbinden“ an. Zwei weitere Listen stehen über dieser Auswahl und dem Routing-Set: „Immer über VPN“ (der gesamte Internetverkehr der App läuft über den Server, Routing-Regeln gelten für sie nicht; Geräte im Heimnetz bleiben direkt erreichbar) und „Immer direkt“ (die App arbeitet wie ohne VPN). Eine App kann nur in einer Liste stehen. Die Listen öffnen sich auch über „Regeln“. Unter Android 9 und älter sorgt „Immer über VPN“ nur dafür, dass die App im Tunnel ist.

### Einstellungen

Sprache, Design und Akzent, „Verbindung nach dem Verbinden prüfen“, IPv6 durch den Tunnel, MTU, Ausnahmen nach Adresse, Mux, Abkürzungen zu den Systemeinstellungen für Always-on-VPN und Akku, Sicherung, „Über LAVRay“ und Lizenzen. Der Tunnel läuft immer mit gVisor und standardmäßig mit MTU 9000, eine Stack-Auswahl gibt es in den Einstellungen nicht.

### Updates

Die Seite „Updates“ öffnet nur die Release-Seite. Die App fragt das Netz nicht nach neuen Versionen und aktualisiert sich nicht selbst: Lade eine neue APK herunter und installiere sie über die alte, deine Daten bleiben erhalten.

### Sicherung

„Einstellungen“ → „Sicherung“: alle Daten (Server mit ihren Passwörtern, Gruppen, Abos, eigene Sets, Einstellungen) in einer `.lavbk`-Datei unter deinem Passwort. Ohne das Passwort lässt sich die Datei nicht öffnen. Die Wiederherstellung ersetzt alle aktuellen Daten und verlangt, dass du das VPN vorher trennst.

---

## Autostart, Always-on und Unterschiede zwischen Geräten

- Schalte „Beim Start des Telefons verbinden“ ein: Nach einem Neustart verbindet sich die App mit dem zuletzt genutzten Server. Android sendet das Boot-Signal erst nach der **ersten Entsperrung** des Telefons.
- Ist das Telefon ohne Netz gestartet (zum Beispiel im Flugmodus), wartet die App still und verbindet sich, sobald ein Netz da ist. Es gibt keine Timer und kein Polling.
- Am zuverlässigsten ist das systemweite **Always-on-VPN**. In LAVRay: „Einstellungen“ → „System“ → „VPN-Einstellungen öffnen“. Es öffnet sich die VPN-Liste von Android: Tippe auf das Zahnrad neben LAVRay und aktiviere „Always-on-VPN“ und, falls gewünscht, „Verbindungen ohne VPN blockieren“ (die Namen hängen vom Gerät ab). LAVRay ändert diese Schalter nicht. Die Blockierung berücksichtigt die App an einer Stelle: Apps aus „Immer direkt“ bleiben dann im Tunnel und umgehen den Server, sonst ließe Android sie ohne Netz. Startet das System die App, behandelt sie das als Always-on-Start und verbindet den zuletzt genutzten Server.
- Eine zwangsweise beendete App erhält das Boot-Signal erst, wenn du sie wieder öffnest. Ein erzwungenes Beenden in den Android-Einstellungen schaltet den Tunnel ab, und er kommt nicht von selbst zurück: Das ist Absicht.
- Beendet das System den VPN-Dienst, startet er neu und baut den Tunnel wieder auf, und die App zeigt einen Hinweis mit der Ursache. Nach einem App-Update kommt ein eingeschalteter Tunnel wieder hoch.
- Herstelleroberflächen (Xiaomi/HyperOS, Huawei/HarmonyOS, Samsung, Oppo/Realme/OnePlus, Vivo) können den Autostart blockieren oder den Dienst bei ausgeschaltetem Bildschirm beenden. Nimm LAVRay dann in die Ausnahmen der Autostart- und Akkuverwaltung auf. Mehr dazu in [docs/PHONE-CHECKLIST.md](docs/PHONE-CHECKLIST.md) (Russisch).
- Nimmt dir eine andere VPN-App die Zustimmung weg, zeigt LAVRay die Benachrichtigung „VPN-Berechtigung erforderlich“: Öffne die App und bestätige erneut.

---

## Messwerte

Alle Zahlen stammen aus [docs/MEASUREMENTS.md](docs/MEASUREMENTS.md) (Russisch) Start, Speicher und Geschwindigkeit wurden **auf einem x86_64-Android-16-Emulator (2 vCPU) gemessen, nicht auf einem Smartphone**; auf einem echten Gerät weichen sie ab. Die APK-Größen stammen aus den Build-Dateien.

| Messgröße | Wert (Emulator) |
|---|---|
| APK-Größe: arm64-v8a / armeabi-v7a / x86_64 | 16,21 / 16,68 / 17,27 MB (aus den Build-Artefakten, nicht vom Emulator; bei 0.9.3 waren es 20,65 / 21,24 / 21,97) |
| Kaltstart der Oberfläche, Median | etwa 1,26 s (1259,5 ms, 16 Starts) |
| Speicher des Prozesses `:vpn` im Leerlauf (verbunden, Bildschirm aus) | etwa 63 MB PSS (bei 0.9.3 etwa 67) |
| Wakelocks, Alarme und Jobs im Leerlauf | keine |
| „Verbinden“-Tipp bis zur ersten Antwort durch den Tunnel, Median | 844 ms (lokaler Testserver) |
| MTU 9000 gegenüber 1500, Durchsatz durch den Tunnel | 4,1-mal schneller im Download, 2,4-mal im Upload (Teststand im Emulator, kein echtes Netz) |

Größe und Speicher wurden für 0.9.4 gemessen; die übrigen Zeilen wurden für 0.9.3 gemessen und nach den Änderungen von 0.9.4 nicht neu gemessen. Nicht gemessen: Akkuverbrauch, Geschwindigkeit auf einem echten Smartphone, Betrieb auf arm64 und armeabi-v7a.

---

## Datenschutz

Viele Abo-Anbieter begrenzen die Zahl der Geräte anhand einer Kennung, die der Client sendet. **LAVRay sendet standardmäßig keine solche Kennung:** Die Abo-Anfrage enthält nur den User-Agent `LAVRay/<Version>`. Verlangt dein Anbieter eine HWID, schalte „Gerätekennung senden (HWID)“ in den Abo-Einstellungen ein: Gesendet wird eine zufällige, für diese Installation erzeugte Kennung, keine Daten deines Telefons.

Die Kamera dient nur zum Lesen von QR-Codes. Die Zwischenablage liest die App nur, wenn du das Importfenster öffnest. Die App-Auswahl listet nur Apps mit Starter-Symbol. Die eigene Android-Datensicherung ist für diese App abgeschaltet.

---

## Was noch nicht funktioniert

- **Kein Selbst-Update:** Neue Versionen installierst du von Hand von der Release-Seite.
- **Einige Desktop-Funktionen fehlen**, darunter der Server-Editor, der „Vollständige Test“ und der Assistent beim ersten Start. Die vollständige Liste mit Gründen steht in [docs/NOT-PORTED.md](docs/NOT-PORTED.md) (Russisch).
- **XHTTP-Transport:** Links werden importiert, der Core unterstützt ihn aber noch nicht.
- **Tor ist aus dem Build entfernt:** Es braucht eine externe Datei, die Android nicht hat. Der Tunnel läuft immer auf gVisor, ohne Stack-Auswahl.
- **Nur das systemweite Always-on-VPN mit „Verbindungen ohne VPN blockieren“ bleibt geschlossen, wenn die App stirbt.** Das Anhalten des Datenverkehrs in der App dauert höchstens 30 Sekunden.
- **Ein Abo auf einer Adresse im lokalen Netz lässt sich nicht anlegen:** Der Core nimmt solche Adressen nicht an.
- **Nicht auf einem echten Smartphone getestet.**

---

## Aus dem Quellcode bauen

Du brauchst JDK 21, das Android SDK (Plattform 36) mit NDK 28.2.13676358, Go (die Version aus `third_party/lavray/core/go.mod`) und Bash.

```bash
git clone --recurse-submodules https://github.com/anatolijlaptev1991-ctrl/lavray-android.git
cd lavray-android
bash scripts/build-core.sh        # Go-Core -> app/libs/lavray-core.aar (gomobile bind)
./gradlew assembleRelease         # APKs in app/build/outputs/apk/release/
```

Ein lokaler Build ist mit dem Debug-Schlüssel signiert und darf nicht weitergegeben werden. Releases baut `release.yml` bei einem Tag `v*`; der Schlüssel ist in [docs/SIGNING.md](docs/SIGNING.md) beschrieben. Aufbau des Codes: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), Technologiewahl: [docs/STACK.md](docs/STACK.md) (beide auf Russisch).

## Fehler melden

[Erstelle ein Issue](../../issues/new/choose): was du getan hast, was du erwartet hast, was stattdessen passiert ist, Telefonmodell, Android-Version, Herstelleroberfläche, die Version von LAVRay („Einstellungen“ → „Über LAVRay“) und das Protokoll („Diagnose“ → „Protokoll“ → „Teilen“).

## Quellcode und Lizenz

Der App-Code (Kotlin, Compose) und der Core-Build liegen in diesem Repository. Der Go-Core und die Desktop-Oberfläche liegen in [lavray](https://github.com/anatolijlaptev1991-ctrl/lavray) und sind als Submodul `third_party/lavray` eingebunden (Tag v0.9.3).

Lizenz [GPL-3.0-only](LICENSE). LAVRay basiert auf nekoray/NekoBox (GPL-3.0) und sing-box (GPL-3.0 oder neuer).
