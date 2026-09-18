# PiIO Lab

PiIO Lab adalah aplikasi CLI berbasis Go untuk menguji, memantau, dan
menginisialisasi ulang perangkat USB pada Raspberry Pi.

## Daftar Isi

- [Overview Sistem](#overview-sistem)
- [Arsitektur Sistem](#arsitektur-sistem)
- [Struktur Aplikasi / Source Code](#struktur-aplikasi--source-code)
- [Database](#database-kalau-ada)
- [API Documentation](#api-documentation-kalau-ada)
- [Business Logic & Data Flow](#business-logic--data-flow-kalau-ada)
- [Authentication & Security](#authentication--security-kalau-ada)
- [Installation & Configuration](#installation--configuration)
- [Deployment](#deployment-kalau-ada)
- [Logging](#logging)
- [Testing](#testing-kalau-ada)

## Overview Sistem

Sistem dirancang untuk Raspberry Pi 4 dan Raspberry Pi OS. Perangkat yang
didukung oleh konfigurasi bawaan adalah:

| Perangkat | Interface | Fungsi |
| --- | --- | --- |
| EPSON TM-T82X | USB Printer/ESC-POS | Inisialisasi dan cetak struk uji |
| Honeywell HF600G2/HF680 | USB Keyboard atau USB Serial | Membaca QR code |
| BOYA BY-MM1+ melalui USB Sound Card | ALSA capture | Merekam audio mono ke WAV |
| ESP32 | USB CDC-ACM serial | Membaca log serial `115200 8N1` |

Ketika aplikasi dimulai, perangkat diinisialisasi secara berurutan: printer,
scanner, audio, lalu ESP32. Kegagalan satu perangkat tidak menghentikan
inisialisasi perangkat lainnya.

Setelah inisialisasi, aplikasi terus memantau event USB Linux. Perangkat yang
dicabut akan ditandai `DISCONNECTED`. Ketika dipasang kembali, node Linux dicari
ulang dan perangkat diinisialisasi kembali secara otomatis.

Menu aplikasi:

1. `Test printer`
2. `Listen QR scanner`
3. `Record microphone to WAV`
4. `Monitor ESP32 log`
5. `Show device status`
0. `Exit`

Operasi menu dijalankan satu per satu, sedangkan pemantauan hot-plug USB tetap
berjalan di background.

## Arsitektur Sistem

PiIO Lab menggunakan pemisahan bergaya clean architecture:

```text
┌──────────────────────────────────────────┐
│ Presentation: CLI                       │
│ Menu, prompt, dan tampilan status       │
├──────────────────────────────────────────┤
│ Application                             │
│ Discovery, state, retry, dan hot-plug   │
├──────────────────────────────────────────┤
│ Domain                                  │
│ Model dan kontrak/interface             │
├──────────────────────────────────────────┤
│ Infrastructure: Linux                   │
│ sysfs, netlink, evdev, serial, ALSA     │
└──────────────────────────────────────────┘
```

`main.go` berfungsi sebagai composition root. File tersebut memuat konfigurasi,
membuat implementasi repository/initializer/event watcher, menghubungkan
application manager dengan CLI, serta mengelola lifecycle dan sinyal shutdown.

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
│   │   ├── manager.go
│   │   └── manager_test.go
│   ├── infrastructure/
│   │   └── linux/
│   │       ├── console.go
│   │       ├── devices.go
│   │       ├── devices_test.go
│   │       ├── keyboard.go
│   │       ├── serial.go
│   │       └── usb.go
│   └── presentation/
│       └── cli/
│           ├── menu.go
│           └── menu_test.go
├── logs/                 # Dibuat otomatis
└── recordings/           # Dibuat otomatis saat merekam
```

Tanggung jawab setiap bagian:

- `domain`: model konfigurasi, perangkat USB, status, dan interface inti.
- `config`: nilai bawaan dan validasi `config.json`.
- `application`: discovery, state management, retry, disconnect, dan reconnect.
- `infrastructure/linux`: akses perangkat dan fasilitas kernel Linux.
- `presentation/cli`: menu, input pengguna, dan orkestrasi pengujian mandiri.
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
- `DeviceInitializer`: menginisialisasi perangkat yang ditemukan.
- `USBEventWatcher`: menerima perubahan perangkat USB.
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
tampilkan menu dan mulai monitor USB
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

### Printer

Startup mengirim `ESC @` untuk menginisialisasi printer. Menu tes hanya mengirim
struk dan perintah potong jika pengguna mengonfirmasi bahwa kertas sudah
terpasang. Tanpa konfirmasi, tidak ada data cetak yang dikirim.

### QR scanner

Dalam mode `keyboard`, input dibaca dari evdev dan perangkat scanner di-grab
secara eksklusif selama mode listen. Dalam mode `serial`, data dibaca per baris
dari port serial. Setiap hasil scan ditampilkan dan ditulis ke log sebagai
`[QR]`.

### Audio

Audio direkam melalui `arecord` sebagai mono PCM 16-bit 48 kHz. Selama proses,
hasil ditulis ke file `.wav.partial`. Jika selesai normal, file diubah menjadi
`.wav`. Jika Enter ditekan, perangkat terputus, atau proses gagal, file parsial
dihapus.

### ESP32

ESP32 dibuka pada `115200 8N1`. Pembacaan serial bersifat non-blocking dan setiap
baris ditampilkan serta dicatat sebagai `[ESP32]`. Enter dari terminal lokal
atau SSH menghentikan monitor dan kembali ke menu.

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

- Raspberry Pi 4.
- Raspberry Pi OS berbasis Linux, 32-bit atau 64-bit.
- Go 1.22 atau lebih baru.
- Paket `alsa-utils` untuk `arecord`.
- USB Sound Card dengan microphone input untuk BOYA BY-MM1+.

### Instalasi paket

```sh
sudo apt update
sudo apt install golang alsa-utils
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

### Menjalankan

```sh
cd ~/piio-lab
go run main.go
```

Seluruh folder proyek harus tersedia. Jangan menyalin `main.go` saja karena file
tersebut mengimpor package di dalam direktori `internal/`.

## Deployment

Untuk penggunaan tanpa `go run`, build binary langsung pada Raspberry Pi:

```sh
cd ~/piio-lab
go build -o piio-lab main.go
./piio-lab
```

Cross-compile dari Linux untuk Raspberry Pi OS 64-bit:

```sh
GOOS=linux GOARCH=arm64 go build -o dist/piio-lab-arm64 main.go
```

Untuk Raspberry Pi OS 32-bit:

```sh
GOOS=linux GOARCH=arm GOARM=7 go build -o dist/piio-lab-armv7 main.go
```

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
| `[INIT FAILED]` | Inisialisasi ulang gagal |
| `[NODE UPDATED]` | Node Linux atau alias stabil tersedia |
| `[QR]` | Data hasil scan |
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
- validasi jawaban konfirmasi printer.

Pengujian integrasi perangkat dilakukan langsung pada Raspberry Pi dengan
mencabut dan memasang ulang setiap USB, lalu memastikan transisi
`DISCONNECTED → CONNECTED → REINITIALIZING → READY` tercatat.

Build ARM dapat diverifikasi dengan:

```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/piio-arm64 main.go
GOOS=linux GOARCH=arm GOARM=7 go build -o /tmp/piio-armv7 main.go
```

## Catatan Operasional

- Port seperti `1-1.1` dan `1-1.3` merupakan topologi port USB Linux.
- Enter dari SSH didukung untuk menghentikan scanner, ESP32, dan rekaman audio.
- Raspberry Pi tidak mempunyai microphone input pada jack 3,5 mm; BOYA BY-MM1+
  harus masuk melalui USB Sound Card.
- Jika TM-T82X terlihat di `lsusb` tetapi `/dev/usb/lp0` tidak muncul, periksa
  `usblp` dengan `lsmod | grep usblp` dan jalankan `sudo modprobe usblp`.
