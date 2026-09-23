package nusapala

import "errors"

var errQRTooLong = errors.New("QR melebihi 4096 byte; data dibuang")
