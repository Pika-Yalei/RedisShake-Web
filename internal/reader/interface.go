package reader

import (
	"context"
	"github.com/Pika-Yalei/RedisShake-Web/internal/entry"
	"github.com/Pika-Yalei/RedisShake-Web/internal/status"
)

type Reader interface {
	status.Statusable
	StartRead(ctx context.Context) []chan *entry.Entry
}
