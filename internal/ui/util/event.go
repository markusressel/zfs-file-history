package util

import "zfs-file-history/internal/ui/status_message"

type ErrorEvent struct {
	Message *status_message.StatusMessage
}
