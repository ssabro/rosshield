// Package sqlite는 modernc.org/sqlite 드라이버를 storage.Storage 인터페이스로 어댑팅합니다.
//
// PRAGMA는 매 connection 확립 직후 적용됩니다 (커스텀 driver.Connector 래퍼).
// R1 노트 §1·§2 결정에 따라 modernc.org/sqlite (pure Go, CGO 없음)를 채택.
package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"

	mcsqlite "modernc.org/sqlite"
)

// pragmas는 모든 새 connection에 순서대로 적용됩니다 (R1 노트 §2).
var pragmas = []string{
	"PRAGMA foreign_keys = ON",
	"PRAGMA journal_mode = WAL",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA temp_store = MEMORY",
	"PRAGMA cache_size = -20000",
	"PRAGMA wal_autocheckpoint = 1000",
}

// connector는 modernc.org/sqlite의 driver.Driver를 driver.Connector로 어댑팅하면서
// 매 connection 확립 직후 PRAGMA 블록을 실행합니다.
//
// modernc.org/sqlite v1.49는 driver.DriverContext.OpenConnector를 노출하지 않으므로
// 표준 driver.Driver.Open을 직접 사용합니다.
type connector struct {
	drv *mcsqlite.Driver
	dsn string
}

func newConnector(dsn string) driver.Connector {
	return &connector{drv: &mcsqlite.Driver{}, dsn: dsn}
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("sqlite: driver conn does not implement ExecerContext")
	}
	for _, p := range pragmas {
		if _, err := execer.ExecContext(ctx, p, nil); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("sqlite: applying %q: %w", p, err)
		}
	}
	return conn, nil
}

func (c *connector) Driver() driver.Driver {
	return c.drv
}
