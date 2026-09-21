# PiIO Lab

PiIO Lab adalah aplikasi CLI berbasis Go untuk menguji, memantau, dan
menginisialisasi ulang perangkat USB pada Raspberry Pi maupun komputer Linux
x86-64.

## Daftar Isi

- [Overview Sistem](#overview-sistem)
- [Arsitektur Sistem](#arsitektur-sistem)
- [Struktur Aplikasi / Source Code](#struktur-aplikasi--source-code)
- [Database](#database)
- [API Documentation](#api-documentation)
- [Business Logic & Data Flow](#business-logic--data-flow)
- [Authentication & Security](#authentication--security)
- [Installation & Configuration](#installation--configuration)
- [Deployment](#deployment)
- [Logging](#logging)
- [Testing](#testing)

## Overview Sistem

Target utama sistem adalah Raspberry Pi 4 dan Raspberry Pi OS, dengan dukungan
tambahan untuk Linux x86-64 (`amd64`). Perangkat yang didukung oleh konfigurasi
bawaan adalah:

| Perangkat | Interface | Fungsi |
| --- | --- | --- |
| EPSON TM-T82X | USB Printer/ESC-POS | Memeriksa kertas dan mencetak isi QR |
| Honeywell HF600G2/HF680 | USB Keyboard atau USB Serial | Membaca QR code |
| BOYA BY-MM1+ melalui USB Sound Card | ALSA capture | Merekam audio mono ke WAV |
| ESP32 | USB CDC-ACM serial | Membaca log serial `115200 8N1` |

Ketika aplikasi dimulai, perangkat diinisialisasi secara berurutan: printer,
scanner, audio, lalu ESP32. Kegagalan satu perangkat tidak menghentikan
inisialisasi perangkat lainnya.

Setelah inisialisasi, aplikasi terus memantau event USB Linux. Perangkat yang
dicabut akan ditandai `DISCONNECTED`. Ketika dipasang kembali, node Linux dicari
ulang dan perangkat diinisialisasi kembali secara otomatis.

Setelah inisialisasi, listener QR scanner dan monitor log ESP32 langsung aktif
di background. Hasil QR otomatis dikirim ke printer jika printer berstatus
`READY`. Menu aplikasi hanya berisi:

1. `Record Microphone to WAV`
2. `Show Device Status`
3. `Exit`

Perekaman audio, listener scanner, monitor ESP32, pemantauan hot-plug, dan
proses cetak dapat berjalan bersamaan. Request cetak diproses satu per satu
melalui antrean agar data printer tidak saling bercampur.

## Arsitektur Sistem

PiIO Lab menggunakan pemisahan bergaya clean architecture:

```text
┌──────────────────────────────────────────┐
│ Presentation: CLI                       │
│ Menu, prompt, dan tampilan status       │
├──────────────────────────────────────────┤
│ Application                             │
│ State, hot-plug, worker, dan antrean    │
├──────────────────────────────────────────┤
│ Domain                                  │
│ Model dan kontrak/interface             │
├──────────────────────────────────────────┤
│ Infrastructure: Linux                   │
│ sysfs, netlink, evdev, serial, ALSA     │
└──────────────────────────────────────────┘
```

`main.go` berfungsi sebagai composition root. File tersebut memuat konfigurasi,
membuat implementasi repository/initializer/event watcher/gateway, menghubungkan
application manager dan background worker dengan CLI, serta mengelola lifecycle
dan sinyal shutdown.

Application manager menggunakan interface domain sehingga logika status
perangkat tidak bergantung langsung pada detail pemindaian sysfs atau driver
Linux.

## Struktur Aplikasi / Source Code

```text
piio-lab/
├── main.go
├── config.json
├── go.mod
├── README.md
├── internal/
│   ├── domain/
│   │   └── model.go
│   ├── config/
│   │   └── config.go
│   ├── application/
│   │   ├── background.go
│   │   ├── background_test.go
│   │   ├── manager.go
│   │   └── manager_test.go
│   ├── infrastructure/
│   │   └── linux/
│   │       ├── console.go
│   │       ├── devices.go
│   │       ├── devices_test.go
│   │       ├── gateway.go
│   │       ├── keyboard.go
│   │       ├── printer.go
│   │       ├── printer_test.go
│   │       ├── serial.go
│   │       └── usb.go
│   └── presentation/
│       └── cli/
│           └── menu.go
├── logs/                 # Dibuat otomatis
└── recordings/           # Dibuat otomatis saat merekam
```

Tanggung jawab setiap bagian:

- `domain`: model konfigurasi, perangkat USB, status, dan interface inti.
- `config`: nilai bawaan dan validasi `config.json`.
- `application`: discovery, state management, retry, background listener,
  antrean cetak, disconnect, dan reconnect.
- `infrastructure/linux`: akses perangkat dan fasilitas kernel Linux.
- `presentation/cli`: menu, status, dan perekaman audio.
- `main.go`: dependency wiring, logger, context, dan signal handling.

## Database

Sistem tidak menggunakan database.

Status perangkat disimpan di memori selama aplikasi berjalan. Rekaman audio dan
log disimpan sebagai file lokal:

- `recordings/recording-YYYYMMDD-HHMMSS.wav`
- `logs/piio-YYYYMMDD.log`

Status perangkat akan dibangun ulang dari sysfs ketika aplikasi dijalankan
kembali.

## API Documentation

Sistem tidak menyediakan HTTP API, REST API, RPC, maupun socket API. Semua
interaksi dilakukan melalui CLI dan perangkat Linux lokal.

Interface internal utama berada di package `internal/domain`, yaitu:

- `USBRepository`: mencari perangkat USB.
- `DeviceInitializer`: menginisialisasi dan memeriksa kesiapan perangkat.
- `USBEventWatcher`: menerima perubahan perangkat USB.
- `PeripheralGateway`: membaca scanner/serial dan mengirim hasil QR ke printer.
- `Logger`: mencatat aktivitas aplikasi.

Interface tersebut merupakan kontrak internal Go dan bukan API jaringan.

## Business Logic & Data Flow

### Startup

```text
config.json
    ↓
scan /sys/bus/usb/devices
    ↓
cocokkan VID/PID dan nama perangkat
    ↓
printer → scanner → audio → ESP32
    ↓
simpan status CONNECTED/READY/ERROR
    ↓
mulai listener scanner + monitor ESP32 + worker printer
    ↓
tampilkan menu
```

### Disconnect dan reconnect

```text
kernel netlink event / periodic polling
    ↓
scan ulang USB dan node Linux
    ↓
DISCONNECTED atau CONNECTED
    ↓
retry sampai node/permission tersedia
    ↓
READY atau INIT FAILED
```

Nama node seperti `/dev/ttyACM0`, `/dev/input/event1`, dan
`/dev/snd/pcmC3D0c` tidak dianggap permanen. Setelah reconnect, sistem mencari
node terbaru dan mengutamakan alias stabil `/dev/serial/by-id/` serta
`/dev/input/by-id/`.

### QR ke printer

```text
scanner membaca QR
    ↓
catat [QR]
    ↓
printer READY? ── tidak ──→ batalkan dan catat [PRINT SKIPPED]
    │ ya
    ↓
masukkan request ke antrean
    ↓
periksa ulang READY dan sensor kertas
    ↓
cetak isi QR atau catat kegagalan
```

Printer diinisialisasi dengan `ESC @`. Sensor roll paper dibaca menggunakan
perintah real-time status ESC/POS `DLE EOT n=4` saat inisialisasi, secara
periodik, dan sekali lagi tepat sebelum mencetak. Kondisi kertas habis atau
tidak terpasang membuat printer berstatus tidak siap. Request cetak tersebut
dibatalkan, tidak dicoba ulang, dan hasilnya dicatat ke log. Setelah kertas
dipasang, pemeriksaan berikutnya akan menginisialisasi printer kembali.

### QR scanner

Dalam mode `keyboard`, input dibaca dari evdev dan perangkat scanner di-grab
secara eksklusif selama aplikasi berjalan. Dalam mode `serial`, data dibaca per
baris dari port serial. Listener aktif otomatis di background dan dibuka ulang
setelah perangkat reconnect. Setiap hasil scan ditampilkan dan ditulis ke log
sebagai `[QR]`, kemudian dibuat menjadi request cetak.

### Audio

Audio direkam melalui `arecord` sebagai mono PCM 16-bit 48 kHz. Selama proses,
hasil ditulis ke file `.wav.partial`. Jika selesai normal, file diubah menjadi
`.wav`. Jika Enter ditekan, perangkat terputus, atau proses gagal, file parsial
dihapus.

### ESP32

ESP32 dibuka pada `115200 8N1`. Pembacaan serial bersifat non-blocking, aktif
otomatis di background, dan setiap baris ditampilkan serta dicatat sebagai
`[ESP32]`. Monitor dibuka kembali setelah perangkat reconnect.

## Authentication & Security

Aplikasi tidak memiliki mekanisme login atau authentication sendiri. Akses
dikendalikan oleh user dan group Linux.

User yang menjalankan aplikasi perlu tergabung dalam group berikut:

| Group | Akses |
| --- | --- |
| `lp` | Printer `/dev/usb/lp*` |
| `input` | Scanner `/dev/input/event*` |
| `audio` | Capture `/dev/snd/*` |
| `dialout` | Serial `/dev/ttyACM*` atau `/dev/ttyUSB*` |

Jalankan aplikasi sebagai user biasa, bukan dengan `sudo`. Pembatasan group
memberikan akses minimum yang diperlukan.

Perhatikan bahwa QR scan dan log ESP32 ditulis ke file log. Jika data tersebut
bersifat sensitif, batasi permission direktori `logs/`, atur rotasi/retensi, dan
jangan membagikan file log tanpa pemeriksaan.

## Installation & Configuration

### Prasyarat

- Raspberry Pi 4 dengan Raspberry Pi OS 32-bit/64-bit, atau komputer Linux
  x86-64 (`amd64`).
- Paket `alsa-utils` untuk `arecord`.
- USB Sound Card dengan microphone input untuk BOYA BY-MM1+.
- Go 1.22 atau lebih baru hanya diperlukan untuk instalasi dari source atau
  melakukan build sendiri. Instalasi binary release tidak memerlukan Go.

### Persiapan sistem

```sh
sudo apt update
sudo apt install curl alsa-utils
sudo usermod -aG lp,input,audio,dialout "$USER"
sudo modprobe usblp
sudo reboot
```

Setelah reboot, verifikasi perangkat:

```sh
groups
lsusb
ls -l /dev/usb/lp*
ls -l /dev/input/by-id/
ls -l /dev/serial/by-id/
ls -l /dev/snd/
```

### Instalasi melalui binary release (direkomendasikan)

Metode ini tidak memerlukan Git atau Go. Tentukan arsitektur sistem Linux:

```sh
uname -m
```

| Hasil | Paket release |
| --- | --- |
| `aarch64` | `linux-arm64` |
| `armv7l` | `linux-armv7` |
| `x86_64` | `linux-amd64` |

Contoh berikut menggunakan paket Raspberry Pi OS 64-bit:

```sh
mkdir -p ~/piio-lab
cd ~/piio-lab

PIIO_VERSION=v0.2.0
PIIO_ARCH=linux-arm64

curl -fLO "https://github.com/octarudin/piio-lab/releases/download/${PIIO_VERSION}/piio-lab-${PIIO_VERSION}-${PIIO_ARCH}.tar.gz"
curl -fLO "https://github.com/octarudin/piio-lab/releases/download/${PIIO_VERSION}/SHA256SUMS.txt"

sha256sum -c SHA256SUMS.txt --ignore-missing
tar -xzf "piio-lab-${PIIO_VERSION}-${PIIO_ARCH}.tar.gz"
chmod +x piio-lab
```

Untuk Raspberry Pi OS 32-bit, gunakan:

```sh
PIIO_ARCH=linux-armv7
```

Untuk Linux x86-64, gunakan:

```sh
PIIO_ARCH=linux-amd64
```

Pastikan hasil verifikasi checksum menunjukkan `OK`. Isi hasil ekstraksi:

```text
piio-lab
config.json
README.md
```

Jalankan binary dari direktori yang berisi `config.json`:

```sh
cd ~/piio-lab
./piio-lab
```

### Instalasi melalui Git/source code

Metode ini ditujukan untuk development atau ketika aplikasi ingin dikompilasi
langsung pada sistem Linux yang didukung:

```sh
sudo apt install git golang
cd ~
git clone https://github.com/octarudin/piio-lab.git
cd piio-lab
go test ./...
go run main.go
```

Seluruh folder proyek harus tersedia. `main.go` mengimpor package dalam
direktori `internal/`, sehingga file tersebut tidak dapat disalin sendirian.

### Konfigurasi perangkat

Konfigurasi berada di `config.json`. VID/PID dapat diperoleh melalui `lsusb`.
Konfigurasi bawaan:

| Perangkat | VID:PID |
| --- | --- |
| EPSON TM-T82X | `04b8:0e27` |
| Honeywell HF600G2/HF680 | `23d0:0ce0` |
| C-Media/Unitek USB Audio Adapter | `0d8c:0014` |
| Espressif USB JTAG/serial | `303a:1001` |

Mode scanner keyboard:

```json
"scanner": {
  "mode": "keyboard",
  "baud_rate": 115200
}
```

Setelah scanner dikonfigurasi sebagai USB Serial/COM:

```json
"scanner": {
  "mode": "serial",
  "baud_rate": 115200
}
```

Interval pemeriksaan cadangan ditentukan oleh `monitor_interval_ms`. Kernel
event tetap menjadi mekanisme utama; polling digunakan sebagai cadangan.

### Menjalankan setelah instalasi

Binary release:

```sh
cd ~/piio-lab
./piio-lab
```

Source code:

```sh
cd ~/piio-lab
go run main.go
```

## Deployment

Untuk penggunaan tanpa `go run`, build binary langsung pada Raspberry Pi:

```sh
cd ~/piio-lab
go build -o piio-lab main.go
./piio-lab
```

Cross-compile untuk Raspberry Pi OS 64-bit:

```sh
GOOS=linux GOARCH=arm64 go build -o dist/piio-lab-arm64 main.go
```

Untuk Raspberry Pi OS 32-bit:

```sh
GOOS=linux GOARCH=arm GOARM=7 go build -o dist/piio-lab-armv7 main.go
```

Untuk Linux x86-64:

```sh
GOOS=linux GOARCH=amd64 go build -o dist/piio-lab-amd64 main.go
```

### Membuat aset GitHub Release

PowerShell script yang tersedia akan membangun target ARM64, ARMv7, dan AMD64,
membuat archive `.tar.gz`, serta menghasilkan `SHA256SUMS.txt`. Jalankan dari
root repository pada commit yang sudah diberi tag:

```powershell
.\scripts\build-release.ps1
```

Versi secara default diambil dari tag yang tepat menunjuk ke `HEAD`. Versi juga
dapat diberikan secara eksplisit:

```powershell
.\scripts\build-release.ps1 v0.2.0
```

Seluruh file yang perlu diunggah ke GitHub Release tersedia di direktori
`dist/`.

Salin binary dan `config.json` ke direktori yang sama. Aplikasi bersifat
interaktif, sehingga lebih sesuai dijalankan dari terminal lokal atau SSH.
Menjalankannya sebagai service background tidak direkomendasikan tanpa membuat
mode non-interaktif tersendiri.

## Logging

Log ditampilkan di terminal dan ditulis ke:

```text
logs/piio-YYYYMMDD.log
```

Kategori log utama:

| Log | Arti |
| --- | --- |
| `[OK]` | Inisialisasi awal berhasil |
| `[CONNECTED]` | USB baru terdeteksi |
| `[DISCONNECTED]` | USB dilepas |
| `[REINITIALIZING]` | Inisialisasi ulang dimulai |
| `[READY]` | Perangkat siap digunakan |
| `[NOT READY]` | Perangkat terhubung tetapi tidak dapat digunakan |
| `[INIT FAILED]` | Inisialisasi ulang gagal |
| `[NODE UPDATED]` | Node Linux atau alias stabil tersedia |
| `[QR]` | Data hasil scan |
| `[PRINT OK]` | Isi QR berhasil dikirim ke printer |
| `[PRINT SKIPPED]` | Request dibatalkan karena printer tidak siap/kertas habis |
| `[PRINT FAILED]` | Pengiriman data cetak gagal |
| `[ESP32]` | Baris log ESP32 |
| `[READ ERROR]` | Operasi perangkat gagal |
| `[CANCELLED]` | Operasi dibatalkan pengguna |

Log menggunakan timestamp hingga mikrodetik. Rotasi dan penghapusan log lama
belum dilakukan otomatis.

## Testing

Jalankan seluruh unit test:

```sh
go test ./...
```

Jalankan static analysis:

```sh
go vet ./...
```

Test yang tersedia mencakup:

- pencocokan selector VID/PID dan nama perangkat;
- kompatibilitas role dengan node Linux;
- konversi node ALSA menjadi nama perangkat capture;
- pemetaan tombol keyboard scanner;
- pembacaan serial non-blocking dan penghentian melalui context;
- interpretasi status sensor kertas printer;
- pembatalan request cetak ketika printer tidak `READY`.

Pengujian integrasi perangkat dilakukan langsung pada Raspberry Pi dengan
mencabut dan memasang ulang setiap USB, lalu memastikan transisi
`DISCONNECTED → CONNECTED → REINITIALIZING → READY` tercatat.

Build untuk seluruh target release dapat diverifikasi dengan:

```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/piio-arm64 main.go
GOOS=linux GOARCH=arm GOARM=7 go build -o /tmp/piio-armv7 main.go
GOOS=linux GOARCH=amd64 go build -o /tmp/piio-amd64 main.go
```

## Catatan Operasional

- Port seperti `1-1.1` dan `1-1.3` merupakan topologi port USB Linux.
- Enter dari terminal lokal atau SSH membatalkan rekaman audio. Listener scanner
  dan monitor ESP32 tetap aktif di background.
- Raspberry Pi tidak mempunyai microphone input pada jack 3,5 mm; BOYA BY-MM1+
  harus masuk melalui USB Sound Card.
- Jika TM-T82X terlihat di `lsusb` tetapi `/dev/usb/lp0` tidak muncul, periksa
  `usblp` dengan `lsmod | grep usblp` dan jalankan `sudo modprobe usblp`.
