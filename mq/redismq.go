package mq

// currently we use redis
import (
	//"fmt"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/superisaac/jsoff"
	"net/url"
	"strconv"
	"time"
)

func streamsKey(section string) string {
	return "rpcmq:" + section
}

func xmsgStr(xmsg *redis.XMessage, key string) string {
	if v, ok := xmsg.Values[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func convertXMsgs(xmsgs []redis.XMessage, defaultOffset string, offsetOnly bool) MQChunk {
	items := []MQItem{}
	lastOffset := defaultOffset
	for _, xmsg := range xmsgs {
		lastOffset = xmsg.ID
		kind := xmsgStr(&xmsg, "kind")
		if kind == "" {
			continue
		}
		item := MQItem{
			Offset:  xmsg.ID,
			Kind:    kind,
			Brief:   xmsgStr(&xmsg, "brief"),
			MsgData: []byte(xmsgStr(&xmsg, "msgdata")),
		}
		items = append(items, item)
	}

	if offsetOnly {
		items = []MQItem{}
	}
	return MQChunk{
		Items:      items,
		LastOffset: lastOffset,
	}
}

func redisOptions(u *url.URL) (*redis.Options, error) {
	if u.Scheme != "redis" {
		return nil, errors.New("scheme is not redis")
	}

	sdb := u.Path[1:]
	db := 0
	if sdb != "" {
		var err error
		db, err = strconv.Atoi(sdb)
		if err != nil {
			return nil, errors.Wrap(err, "strconv.Atoi")
		}
	}
	pwd, ok := u.User.Password()
	if !ok {
		pwd = ""
	}
	opt := &redis.Options{
		Addr:     u.Host,
		Password: pwd,
		DB:       db,
	}
	return opt, nil
}

type RedisMQClient struct {
	rdb *redis.Client
}

func NewRedisClient(redisUrl *url.URL) (*redis.Client, error) {
	opts, err := redisOptions(redisUrl)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}

func NewRedisMQClient(mqurl *url.URL) *RedisMQClient {
	c, err := NewRedisClient(mqurl)
	if err != nil {
		panic(err)
	}
	return &RedisMQClient{
		rdb: c,
	}
}

func (self RedisMQClient) Add(ctx context.Context, section string, ntf *jsoff.NotifyMessage) (string, error) {
	kind := "Notify"
	brief := ntf.MustMethod()
	values := map[string]interface{}{
		"kind":    kind,
		"brief":   brief,
		"msgdata": jsoff.MessageString(ntf),
	}
	addedID, err := self.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamsKey(section),
		Values: values,
		MaxLen: 10000,
	}).Result()
	if err != nil {
		return "", errors.Wrap(err, "redis.XAdd")
	}
	return addedID, nil
}

func (self RedisMQClient) Chunk(ctx context.Context, section string, prevID string, count int64) (MQChunk, error) {
	if count <= 0 {
		log.Panicf("count %d <= 0", count)
	}
	skey := streamsKey(section)
	if prevID == "" {
		// get the last item
		xmsgs, err := self.rdb.XRevRangeN(ctx, skey, "+", "-", 1).Result()
		if err != nil {
			return MQChunk{}, errors.Wrap(err, "redis.XRevRangeN")
		}
		// assert len(msgs) <= 1
		if len(xmsgs) > 1 {
			log.Panicf("xrevrange(%s, +, -, 1) got more than 1 items", skey)
		}
		return convertXMsgs(xmsgs, prevID, true), nil
	} else {
		xmsgs, err := self.rdb.XRangeN(ctx, skey, "("+prevID, "+", count).Result()
		if err != nil {
			return MQChunk{}, errors.Wrap(err, "redis.XRangeN")
		}
		return convertXMsgs(xmsgs, prevID, false), nil
	}
}

func (self RedisMQClient) Tail(ctx context.Context, section string, count int64) (MQChunk, error) {
	if count <= 0 {
		log.Panicf("count %d <= 0", count)
	}

	revmsgs, err := self.rdb.XRevRangeN(ctx, streamsKey(section), "+", "-", count).Result()
	if err != nil {
		return MQChunk{}, errors.Wrap(err, "redis.XRevRangeN")
	}

	xmsgs := make([]redis.XMessage, len(revmsgs))
	// revert the list
	for i, xmsg := range revmsgs {
		xmsgs[len(revmsgs)-1-i] = xmsg
	}
	return convertXMsgs(xmsgs, "", false), nil
}

func (self RedisMQClient) Subscribe(rootctx context.Context, section string, output chan MQItem) error {
	ctx, cancel := context.WithCancel(rootctx)

	defer func() {
		log.Debug("subscribe stop")
		cancel()
	}()

	prevID := ""
	for {
		chunk, err := self.Chunk(rootctx, section, prevID, 100)
		if err != nil {
			return err
		}
		prevID = chunk.LastOffset
		if len(chunk.Items) > 0 {
			log.Debugf("got chunk of %d items, lastOffset=%s", len(chunk.Items), chunk.LastOffset)
			for _, item := range chunk.Items {
				output <- item
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Millisecond):
				continue
			}
		}

	}
}
