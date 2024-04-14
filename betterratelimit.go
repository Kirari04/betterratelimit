package betterratelimit

import (
	"net/http"
	"sync"
	"time"

	"log"

	"github.com/labstack/echo/v4"
)

type BetterRatelimitGlobalConfig struct {
	Skipper                            func(c echo.Context) bool
	DefaultRatelimit                   uint
	BlockPathAfterNPercentIncrease     uint // this value should be higher than 101
	CheckBlockPathAccrosLastNSeconds   uint // this value will check for the increase (BlockPathAfterNPercentIncrease) insidded the last N seconds
	BlockPathEnableCheckAfterNRequests uint // this value will enable the increase check if the minimum n requests is met
	BanPathForNTime                    time.Duration
}

// this is the default global ratelimit config
var DefaultBetterRatelimitGlobalConfig = BetterRatelimitGlobalConfig{
	Skipper:                            func(c echo.Context) bool { return false },
	DefaultRatelimit:                   60,
	BlockPathAfterNPercentIncrease:     200,
	CheckBlockPathAccrosLastNSeconds:   10,
	BlockPathEnableCheckAfterNRequests: 100,
	BanPathForNTime:                    time.Duration(time.Second * 60),
}

type GlobalRatelimitHistory struct {
	sync.RWMutex
	// the history contains a key value store with the key being the current minute
	// and the value is a key value store with the key being the path and the value the number of requests
	history map[time.Time]*GlobalRatelimitHistoryTracker
}

// this function returns the ratelimit map of the current minute
// if non exists yet it creates one
func (c *GlobalRatelimitHistory) Get(timeHash time.Time) *GlobalRatelimitHistoryTracker {
	c.Lock()
	defer c.Unlock()
	history, ok := c.history[timeHash]
	if !ok || history == nil {
		c.history[timeHash] = &GlobalRatelimitHistoryTracker{
			tracker: make(map[string]uint),
		}
	}

	return c.history[timeHash]
}

// this function adds the the path to the history of requests
func (c *GlobalRatelimitHistory) ShouldBlockPath(path string, lastNSeconds uint, maxIncrease uint, miniumRequests uint, banForNTime time.Duration) bool {
	timeHashes := getTimeHashes(lastNSeconds)
	var smallestCount uint = miniumRequests
	var biggestCount uint
	for _, timeHash := range timeHashes {
		history := c.Get(timeHash)
		history.Lock()
		count, ok := history.tracker[path]
		history.Unlock()
		if !ok {
			// path wasn't requested yet
			continue
		}

		if smallestCount > count {
			smallestCount = count
		}
		if biggestCount < count {
			biggestCount = count
		}
	}

	// block if increase is too big
	if smallestCount < miniumRequests {
		return false
	}
	shouldBlock := (float32(100)/float32(smallestCount))*float32(biggestCount) > float32(maxIncrease)
	if shouldBlock {
		globalRatelimitBanPaths.Add(path, banForNTime)
	}
	return shouldBlock
}

// this function adds the the path to the history of requests
func (c *GlobalRatelimitHistory) Append(path string) {
	timeHash := getTimeHash()
	history := c.Get(timeHash)
	history.Append(path, 1)
}

// this function adds the the path with a weight to the history of requests
func (c *GlobalRatelimitHistory) AppendWithWeight(path string, weight uint) {
	timeHash := getTimeHash()
	history := c.Get(timeHash)
	history.Append(path, weight)
}

type GlobalRatelimitHistoryTracker struct {
	sync.RWMutex
	tracker map[string]uint
}

func (c *GlobalRatelimitHistoryTracker) Append(path string, weight uint) {
	c.Lock()
	if _, ok := c.tracker[path]; !ok {
		c.tracker[path] = 0
	}
	c.tracker[path] = c.tracker[path] + (1 * weight)
	c.Unlock()
}

type GlobalRatelimitBanPaths struct {
	sync.RWMutex
	banns map[string]time.Time
}

// add path to banned list for n time
func (c *GlobalRatelimitBanPaths) Add(path string, duration time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.banns[path] = time.Now().Add(duration)
}

// check if path is banned
func (c *GlobalRatelimitBanPaths) IsBanned(path string) bool {
	c.Lock()
	defer c.Unlock()
	t, ok := c.banns[path]
	if !ok {
		return false
	}

	return !t.Before(time.Now())
}

// this var contains the history of requests which is being used to evaluate the requests
var globalRatelimitHistoryTracker *GlobalRatelimitHistory = &GlobalRatelimitHistory{
	history: make(map[time.Time]*GlobalRatelimitHistoryTracker),
}

// keep track of banned paths so we dont have to recalculate the banns on each requests
var globalRatelimitBanPaths *GlobalRatelimitBanPaths = &GlobalRatelimitBanPaths{
	banns: make(map[string]time.Time),
}

// this middleware should be hooked at the root of the router and only be used once
func BetterRatelimitGlobal(config BetterRatelimitGlobalConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}
			if globalRatelimitHistoryTracker == nil {
				log.Printf("globalRatelimitHistoryTracker is nil")
				return next(c)
			}
			globalRatelimitHistoryTracker.Append(c.Path())

			if globalRatelimitBanPaths.IsBanned(c.Path()) {
				return c.NoContent(http.StatusTooManyRequests)
			}

			if globalRatelimitHistoryTracker.ShouldBlockPath(
				c.Path(),
				config.CheckBlockPathAccrosLastNSeconds,
				config.BlockPathAfterNPercentIncrease,
				config.BlockPathEnableCheckAfterNRequests,
				config.BanPathForNTime,
			) {
				return c.NoContent(http.StatusTooManyRequests)
			}

			return next(c)
		}
	}
}

// get the full history of requests
func BetterRatelimitGetHistory() map[time.Time]map[string]uint {
	globalRatelimitHistoryTracker.Lock()
	defer globalRatelimitHistoryTracker.Unlock()
	val := make(map[time.Time]map[string]uint)
	if globalRatelimitHistoryTracker == nil {
		log.Println("globalRatelimitHistoryTracker is nil")
		return val
	}
	for t, h := range globalRatelimitHistoryTracker.history {
		val[t] = h.tracker
	}
	return val
}

// get the history of requests of the current second
func BetterRatelimitGetActiveHistory() map[string]uint {
	if globalRatelimitHistoryTracker == nil {
		return make(map[string]uint)
	}
	timeHash := getTimeHash()
	history := globalRatelimitHistoryTracker.Get(timeHash)
	history.Lock()
	defer history.Unlock()
	return history.tracker
}

func getTimeHash() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.Local)
}

func getTimeHashes(lastNSeconds uint) []time.Time {
	now := time.Now()
	val := make([]time.Time, lastNSeconds)
	for i := 0; i < int(lastNSeconds); i++ {
		nowI := now.Add(time.Second * time.Duration(i) * -1)
		val[i] = time.Date(nowI.Year(), nowI.Month(), nowI.Day(), nowI.Hour(), nowI.Minute(), nowI.Second(), 0, time.Local)
	}
	return val
}
