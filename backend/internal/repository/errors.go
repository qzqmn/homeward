package repository

import "errors"

// ErrNotFound 由各 repository 統一回傳，handler 層據此轉換成 404。
var ErrNotFound = errors.New("repository: not found")

// ErrInvalidChannel 由 ChannelRepo.Register 在 channel 名稱不在白名單時回傳。
var ErrInvalidChannel = errors.New("repository: invalid channel")

// ErrInvalidNotificationKind 由 NotificationRepo.EnqueueToUser 在 kind 不在
// 白名單（會被用來組動態 SQL）時回傳。
var ErrInvalidNotificationKind = errors.New("repository: invalid notification kind")
