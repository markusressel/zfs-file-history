package util

import "zfs-file-history/internal/ui/status_message"

type StatusMessageEvent struct {
	Message *status_message.StatusMessage
}
