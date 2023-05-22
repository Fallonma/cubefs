package sdk

import (
	"context"
	"fmt"
	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/sdk/meta"
	"os"
	"strings"
	"sync"
)

func GetFileInodesByMp(mps []*proto.MetaPartitionView, metaPartitionId uint64, concurrency uint64, modifyTimeMin int64, modifyTimeMax int64, metaProf uint16, exit bool) (inodes []uint64, err error) {
	var (
		mpCount uint64
		wg      sync.WaitGroup
		mu      sync.Mutex
		ch      = make(chan *proto.MetaPartitionView, 1000)
	)
	for _, mp := range mps {
		if metaPartitionId > 0 && mp.PartitionID != metaPartitionId {
			continue
		}
		mpCount++
	}
	if mpCount == 0 {
		return
	}
	wg.Add(int(mpCount))
	go func() {
		for _, mp := range mps {
			if metaPartitionId > 0 && mp.PartitionID != metaPartitionId {
				continue
			}
			ch <- mp
		}
		close(ch)
	}()

	for i := 0; i < int(concurrency); i++ {
		go func() {
			for mp := range ch {
				var inos map[uint64]*proto.MetaInode
				if mp.LeaderAddr == "" {
					fmt.Printf("mp[%v] no leader\n", mp.PartitionID)
					wg.Done()
					return
				}
				mtClient := meta.NewMetaHttpClient(fmt.Sprintf("%v:%v", strings.Split(mp.LeaderAddr, ":")[0], metaProf), false)
				inos, err = mtClient.GetAllInodes(mp.PartitionID)
				if err != nil {
					fmt.Printf("get inodes error: %v, mp: %d\n", err, mp.PartitionID)
					if exit {
						os.Exit(0)
					}
					wg.Done()
					return
				}
				mu.Lock()
				for _, ino := range inos {
					if !proto.IsRegular(ino.Type) {
						continue
					}
					if modifyTimeMin > 0 && ino.ModifyTime < modifyTimeMin {
						continue
					}
					if modifyTimeMax > 0 && ino.ModifyTime > modifyTimeMax {
						continue
					}
					inodes = append(inodes, ino.Inode)
				}
				mu.Unlock()
				wg.Done()
			}
		}()
	}
	wg.Wait()
	return
}

func GetAllInodesByPath(masters []string, vol string, path string) (inodes []uint64, err error) {
	ctx := context.Background()
	var mw *meta.MetaWrapper
	mw, err = meta.NewMetaWrapper(&meta.MetaConfig{
		Volume:        vol,
		Masters:       masters,
		ValidateOwner: false,
		InfiniteRetry: true,
	})
	if err != nil {
		fmt.Printf("NewMetaWrapper fails, err:%v\n", err)
		return
	}
	var ino uint64
	ino, err = mw.LookupPath(ctx, path)
	if err != nil {
		fmt.Printf("LookupPath fails, err:%v\n", err)
		return
	}
	return getChildInodesByParent(mw, vol, ino)
}

func getChildInodesByParent(mw *meta.MetaWrapper, vol string, parent uint64) (inodes []uint64, err error) {
	ctx := context.Background()
	var dentries []proto.Dentry
	dentries, err = mw.ReadDir_ll(ctx, parent)
	if err != nil {
		fmt.Printf("ReadDir_ll fails, err:%v\n", err)
		return
	}
	var newInodes []uint64
	for _, dentry := range dentries {
		if proto.IsRegular(dentry.Type) {
			inodes = append(inodes, dentry.Inode)
		} else if proto.IsDir(dentry.Type) {
			newInodes, err = getChildInodesByParent(mw, vol, dentry.Inode)
			if err != nil {
				return
			}
			inodes = append(inodes, newInodes...)
		}
	}
	return
}
