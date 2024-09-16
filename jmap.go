package keywork

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"time"

	"git.sr.ht/~rockorager/go-jmap"
	"github.com/syndtr/goleveldb/leveldb"
)

type JmapClient struct {
	cl                  *jmap.Client
	db                  *leveldb.DB
	stateChangeDebounce *time.Timer
	emails              map[string]*Email
	name                string
}

func NewJmapClient(name string, url *url.URL) (*JmapClient, error) {
	client := &JmapClient{
		name:   name,
		emails: make(map[string]*Email),
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dbPath := path.Join(cacheDir, "keywork", name+".db")
	client.Log("Cache db path: %q", dbPath)
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		return nil, err
	}
	client.db = db
	return client, nil
}

func (c *JmapClient) Log(format string, v ...any) {
	message := format
	if len(v) > 0 {
		message = fmt.Sprintf(format, v...)
	}
	log.Printf("[%s] %s", c.name, message)
}

func (c *JmapClient) Name() string {
	return c.name
}
