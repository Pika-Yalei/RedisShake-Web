package writer

import (
	"context"
	"github.com/Pika-Yalei/RedisShake-Web/internal/entry"
	"github.com/Pika-Yalei/RedisShake-Web/internal/status"
)

type Writer interface {
	status.Statusable
	Write(entry *entry.Entry)
	StartWrite(ctx context.Context) (ch chan *entry.Entry)
	Close()
}
