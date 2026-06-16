package data

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestCleanupDataWithoutDB(t *testing.T) {
	d := &Data{
		RDB: redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}),
	}

	cleanupData(d)()
}
