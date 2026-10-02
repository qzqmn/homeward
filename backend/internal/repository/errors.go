package repository

import "errors"

// ErrNotFound 由各 repository 統一回傳，handler 層據此轉換成 404。
var ErrNotFound = errors.New("repository: not found")

// ErrInvalidChannel 由 ChannelRepo.Register 在 channel 名稱不在白名單時回傳。
var ErrInvalidChannel = errors.New("repository: invalid channel")
