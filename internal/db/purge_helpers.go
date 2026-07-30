package db

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

type serverPurgePlan struct {
	toDelete [][]byte
	toolURNs []string
}

func collectServerPurgePlan(txn *badger.Txn, s *Store, serverName string) serverPurgePlan {
	var plan serverPurgePlan
	it := txn.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := []byte("tool:" + serverName + ":")
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		key := item.KeyCopy(nil)
		keyStr := string(key)

		itemValueOrWarn(item, func(val []byte) error {
			var r ToolRecord
			if uErr := json.Unmarshal(val, &r); uErr != nil {
				slog.Warn("database: deleting corrupt tool record during purge", "key", keyStr)
				plan.toDelete = append(plan.toDelete, key)
				if len(keyStr) > 5 && strings.HasPrefix(keyStr, "tool:") {
					urn := keyStr[5:]
					plan.toDelete = append(plan.toDelete,
						[]byte("intel:"+urn), []byte("vec:"+urn), []byte("usage:"+urn))
					plan.toolURNs = append(plan.toolURNs, urn)
				}
				return nil
			}
			if r.Server != serverName {
				return nil
			}
			plan.toDelete = append(plan.toDelete, key,
				[]byte("cat:"+r.Category+":"+r.URN),
				[]byte("intel:"+r.URN),
				[]byte("vec:"+r.URN),
				[]byte("usage:"+r.URN))
			plan.toolURNs = append(plan.toolURNs, r.URN)
			s.Cache.Delete("tool:" + r.URN)
			return nil
		})
	}
	return plan
}
