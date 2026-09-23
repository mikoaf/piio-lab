# Mode Nusapala: intercom dan USB berjalan bersamaan

Mode ini dijalankan sebagai binary `piio-nusapala`, terpisah dari CLI PiIO Lab
lama. Tujuannya menguji interaksi driver yang dipakai `nusapala-embedded` pada
Raspberry Pi dengan panggilan dua arah dari browser laptop.

## Yang dijalankan

| Komponen | Implementasi |
| --- | --- |
| Mikrofon dan speaker | Dua blocking stream PortAudio terpisah; capture terus berjalan dan speaker menulis silence saat idle |
| Panggilan | Pion WebRTC v4.2.3 + gopus, audio Opus dua arah, satu laptop per sesi |
| Printer | go-escpos v0.1.0; `Done()` membuka gousb, auto-detach driver, claim interface dan menulis endpoint 1 |
| Scanner | gousb v1.1.3, claim interface USB, baca report HID keyboard langsung |
| STI | go-paymentreader v0.4.4, 38400 8N1, `Initialize` lalu `CheckBalance`, wait timeout 1 detik seperti Nusapala |
| Diagnostik | Status tiap perangkat, jumlah sukses/error, event dengan timestamp, polling inventaris USB/sysfs 2 detik |

Audio dimulai sebelum scanner dan STI, mengikuti urutan Nusapala. Menutup
panggilan hanya menghapus routing WebRTC; PortAudio tetap membaca mikrofon dan
menulis speaker. UI browser menyediakan cetak tes. QR otomatis dicetak jika
`auto_print_qr` aktif. STI melakukan baca kartu/cek saldo, tanpa deduct saldo.

## Build langsung di Raspberry Pi

Gunakan Go **1.24 atau lebih baru**. Dependensi go-paymentreader versi yang sama
dengan Nusapala memerlukan Go 1.24. Library native dan CGO diperlukan:

```sh
sudo apt update
sudo apt install build-essential pkg-config libusb-1.0-0-dev portaudio19-dev libopus-dev

go version
CGO_ENABLED=1 go build -tags nusapala -o piio-nusapala ./cmd/nusapala
```

Pada ARM, gopus memakai `libopus` sistem. Jangan memakai resep cross-compile
`CGO_ENABLED=0` milik CLI lama untuk binary ini. Untuk paket native yang berisi
binary, konfigurasi, dan panduan:

```sh
sh scripts/build-nusapala.sh
```

Hentikan aplikasi Nusapala/CLI lab lain yang sedang memakai perangkat yang sama
sebelum pengujian. QR di-claim secara eksklusif dan perangkat audio default bisa
sudah dipakai aplikasi lain.

## Permission USB, serial, dan audio

Jalankan sebagai user biasa yang memiliki akses perangkat:

```sh
sudo usermod -aG audio,dialout,plugdev "$USER"
```

Buat `/etc/udev/rules.d/99-piio-nusapala.rules` dengan VID/PID perangkat Anda:

```udev
SUBSYSTEM=="usb", ATTR{idVendor}=="04b8", ATTR{idProduct}=="0e27", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="23d0", ATTR{idProduct}=="0ce0", MODE="0660", GROUP="plugdev"
```

Kemudian reload rules dan cabut/pasang kembali USB; login ulang untuk perubahan
group:

```sh
sudo udevadm control --reload-rules
```

Mode ini mengakses USB melalui libusb, bukan `/dev/usb/lp*` atau evdev. Karena
itu, permission group `lp`/`input` saja tidak cukup. Serial STI menggunakan
permission `dialout`.

## Konfigurasi perangkat

File bawaan: `config.nusapala.json`. Untuk file lain:

```sh
./piio-nusapala -config /path/config.nusapala.json
```

Daftar device PortAudio:

```sh
./piio-nusapala -list-audio
```

`audio.input_device` dan `audio.output_device` bernilai `-1` untuk device default,
sama seperti `OpenDefaultStream` di Nusapala. Pilih indeks dari `-list-audio`
jika ingin memastikan sound card yang digunakan. Indeks dapat berubah setelah
reboot/perubahan perangkat; periksa lagi saat itu. Startup gagal audio tidak
menghentikan tes USB; status error tetap tampil.

Audio default: 48000 Hz, mono, frame 480 sampel (10 ms), gain 1. Samakan
`frame_size`, `channels`, dan gain dengan konfigurasi gate yang sedang didiagnosis.
Mode ini menerima frame 120/240/480/960/1920/2880, channels 1 atau 2, dan sample
rate 48000. Jika konfigurasi gate berbeda, hasil uji belum identik. WebRTC
membagi PCM hasil decode sesuai frame PortAudio walaupun ukuran paket browser
berbeda.

Printer dan QR menggunakan VID/PID hex serta `configuration`, `interface`, dan
`alternate`. Samakan scanner `endpoint` dengan `QREndpoint` Nusapala (nomor
endpoint, misalnya **1**, bukan alamat **129/0x81**). Scanner harus mengirim
report **HID boot keyboard 8 byte** dengan layout US. Mode serial scanner tidak
digunakan di mode Nusapala. go-escpos versi ini menetapkan endpoint printer 1.

### STI

Konfigurasi STI sengaja belum aktif sampai port dan key perangkat diisi.

1. Cari port serial STI di `/dev/serial/by-id/` dan isi `sti.port`.
2. Ubah `sti.enabled` menjadi `true`.
3. Isi environment `PIIO_STI_KEY` dengan key inisialisasi STI yang sama dengan
   konfigurasi Nusapala (32 karakter hex). Key tidak disimpan dalam JSON/log.

Contoh input key tanpa menuliskannya pada history shell, dari Bash:

```bash
read -rsp 'STI initialization key: ' PIIO_STI_KEY
printf '\n'
export PIIO_STI_KEY
./piio-nusapala
```

Library memakai timeout respons default 10 detik; wait timeout kartu disetel
1 detik seperti Nusapala. `sti.poll_ms` adalah jeda setelah tiap request selesai,
bukan jaminan interval absolut. Error protokol/no-card/timeout ditampilkan apa
adanya, sehingga status `ERROR` STI tidak otomatis berarti node serial hilang.
Nomor kartu di event aplikasi disamarkan kecuali empat karakter terakhir.

## Panggil dari laptop

Pi dan laptop perlu saling terjangkau di jaringan LAN. Jalankan di Pi:

```sh
./piio-nusapala
```

Di laptop, buka SSH tunnel (sesuaikan username dan IP Pi):

```sh
ssh -N -L 8080:127.0.0.1:8080 pi@192.168.1.50
```

Buka **http://localhost:8080** di Chrome/Firefox, izinkan mikrofon, lalu tekan
**Panggil Raspberry Pi**. Untuk mengakhiri, tekan **Akhiri panggilan**.
Gunakan headset di laptop untuk mengurangi feedback. Jika autoplay ditolak,
tekan Play pada pemutar audio halaman.

Browser memerlukan secure context untuk mikrofon: HTTPS atau localhost.
HTTP langsung ke IP Pi tidak cukup. Rujukan:
[MDN getUserMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia).

SSH tunnel membawa halaman dan signaling HTTP. Audio WebRTC memakai koneksi
langsung **UDP antara laptop dan Pi**; firewall/AP isolation harus mengizinkan
koneksi ini. Mode LAN ini tidak memakai STUN/TURN eksternal dan tidak dirancang
untuk panggilan lintas internet/NAT. Jika signaling berhasil tetapi status tetap
`connecting`, periksa jalur UDP dan jaringan kedua perangkat.

Alternatif: isi `listen` dengan `0.0.0.0:8443`, `tls_cert` dan `tls_key` dengan
sertifikat/key yang dipercaya browser, lalu buka `https://nama-pi:8443`.
Halaman diagnostik tidak memiliki login; binding bawaan loopback + SSH membatasi
akses ke sesi SSH Anda. Gunakan binding LAN hanya pada jaringan pengujian Anda.

Sesi hanya melayani satu panggilan. Laptop mengirim heartbeat setiap 5 detik;
sesi dilepas setelah 20 detik tanpa heartbeat. Refresh/tab tertutup atau
koneksi gagal tidak seharusnya membuat slot panggilan terkunci permanen.

## Skenario reproduksi

1. Jalankan dengan `audio.enabled=false`: scan QR, cetak, tap kartu, simpan log.
2. Restart dengan `audio.enabled=true`, tanpa panggilan; lakukan tes yang sama.
3. Mulai panggilan laptop, bicara dua arah, scan QR/cetak/tap kartu berulang.
4. Akhiri panggilan, ulangi tes perangkat; audio Pi masih persisten.
5. Bandingkan timestamp error pertama di log aplikasi dan log kernel USB.

Di terminal Pi kedua:

```sh
sudo journalctl -kf
```

Log aplikasi: `logs/nusapala-YYYYMMDD.log`, timestamp hingga mikrodetik. Halaman
menampilkan 200 event terakhir. `microphone` dan `speaker` menampilkan jumlah
frame sukses; perangkat lain menampilkan jumlah operasi sukses. Error audio
berulang dicatat maksimal sekali per detik. USB inventory menampilkan node yang
muncul/hilang/berubah, terpisah dari hasil operasi printer/QR/STI.

`printer_usb=CONNECTED` hanya berarti VID/PID dapat dibuka, bukan kertas siap.
`printer=READY` setelah cetak berarti `go-escpos.Done()` berhasil mengirim data,
bukan konfirmasi fisik bahwa kertas tercetak.

## Batas kesamaan dan pemulihan

Mode ini menyamakan driver, pola audio persisten, dan panggilan WebRTC/Opus.
Ia tidak menjalankan UI gate, GPIO, API backend, business flow transaksi,
pemotongan saldo, atau semua logika ducking/jitter buffer Nusapala. Ini alat
isolasi masalah I/O, bukan replika penuh aplikasi gate.

- QR melakukan reconnect dengan jeda 2 detik setelah error. Ia tidak mematikan
  indikator seluruh perangkat atau memanggil `os.Exit` seperti handler Nusapala.
- Printer menggunakan antrean satu worker, library dan lifecycle cetak Nusapala.
  `go-escpos.Done()` tidak menerima context/timeout; jika transfer macet, antrean
  dapat tertahan. Status `PRINTING` membantu mengidentifikasi kondisi itu.
- Audio mencatat error dan terus mencoba stream yang sama. Jika perangkat
  dicabut atau konfigurasi audio berubah, restart program untuk membuka ulang.
- go-paymentreader v0.4.4 tidak menyediakan `Close`. Lab membuka satu reader
  per proses. Setelah error serial/initialization, perbaiki perangkat dan restart;
  lab tidak membuat reader baru berulang kali. OS melepas port saat proses keluar.
- PortAudio/USB/serial memakai perangkat nyata dalam proses yang sama. Error
  internal library tetap mungkin memengaruhi proses seperti pada aplikasi asal.

Jangan menyimpulkan masalah hardware sudah selesai hanya dari unit test.
Reproduksi akhir harus memakai Pi, sound card, printer, scanner, STI, hub, dan
power supply yang sama dengan instalasi bermasalah.

## Pemeriksaan developer

```sh
go test ./...
go test -tags nusapala ./...
go test -race -tags nusapala ./internal/nusapala
go vet -tags nusapala ./...
```

Tes WebRTC memakai dua peer lokal, ICE/DTLS/RTP dan codec Opus asli dengan PCM
sintetis; tidak membuka mikrofon, printer, QR, atau STI. Tes membutuhkan akses
socket/interface jaringan lokal. Tes juga mencakup HID, ukuran frame berbeda,
busy/hangup, pelepasan sesi gagal, HTTP origin check, antrean cetak dan konfigurasi.
