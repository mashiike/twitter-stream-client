package twstclient

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/backoff/v2"
)

type App struct {
	client          *Client
	workerNum       int
	params          url.Values
	output          *lockedWriter
	injectCreatedAt bool
}

func New(config *Config) (*App, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	c, err := NewClient(config)
	if err != nil {
		return nil, err
	}
	app := &App{
		client:          c,
		workerNum:       config.WorkerNum,
		params:          config.QueryParams,
		output:          newLockedWriter(config.Output),
		injectCreatedAt: config.InjectCreatedAt,
	}
	return app, nil
}

func (app *App) Run(ctx context.Context) error {
	waitCh := make(chan chan string, app.workerNum)
	var wg sync.WaitGroup
	workerCtx, workerCancel := context.WithCancel(context.Background())
	for i := 0; i < app.workerNum; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("[DEBUG] werker_id=%d start\n", workerID)
			app.worker(workerCtx, waitCh)
			log.Printf("[DEBUG] werker_id=%d end\n", workerID)
		}()
	}
	done := false
	var err error
	waitSeconds := 5
	for !done {
		startTime := time.Now()
		log.Println("[INFO] start mainloop")
		err = app.mainLoop(ctx, waitCh)
		log.Println("[INFO] end mainloop")
		endTime := time.Now()
		if err != nil {
			log.Printf("[ERROR] %s", err.Error())
			done = true
			continue
		}
		select {
		case <-ctx.Done():
			done = true
			continue
		default:
			elapsed := endTime.Sub(startTime)
			if elapsed < 10*time.Minute {
				waitSeconds *= 2
				if waitSeconds > 320 {
					waitSeconds = 320
				}
			} else {
				waitSeconds = 5
			}
			log.Printf("[INFO] %d sec after try restart mainloop\n", waitSeconds)
			time.Sleep(time.Duration(waitSeconds) * time.Second)
		}
	}
	workerCancel()
	wg.Wait()
	close(waitCh)
	return err
}

func (app *App) mainLoop(ctx context.Context, waitCh <-chan chan string) error {
	respCh, err := app.getStream(ctx)
	if err != nil {
		return err
	}
	log.Println("[INFO] connect success")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	heartbeatCount := 0
	tweetCount := 0
	for {
		select {
		case resp, ok := <-respCh:
			if !ok {
				log.Printf("[INFO] on Disconnect, HeartbeatCount=%d, TweetCount=%d\n", heartbeatCount, tweetCount)
				log.Println("[INFO] disconnect success")
				return nil
			}
			if len(resp) <= 0 {
				heartbeatCount++
				log.Println("[DEBUG] heartbeat")
				continue
			}
			tweetCount++
			tweetCh := <-waitCh
			tweetCh <- resp
		case <-ticker.C:
			log.Printf("[INFO] in 5 Minute, HeartbeatCount=%d, TweetCount=%d\n", heartbeatCount, tweetCount)
			heartbeatCount = 0
			tweetCount = 0
		}
	}
}

func (app *App) worker(ctx context.Context, waitCh chan<- chan string) {
	encoder := json.NewEncoder(app.output)
	inputCh := make(chan string)
	defer func() {
		close(inputCh)
	}()
	for {
		waitCh <- inputCh
		select {
		case <-ctx.Done():
			return
		case input := <-inputCh:
			var data map[string]interface{}
			decoder := json.NewDecoder(strings.NewReader(input))
			if err := decoder.Decode(&data); err != nil {
				log.Printf("[ERROR] %s\n", err.Error())
				continue
			}

			if _, ok := data["errors"]; ok {
				var errs struct {
					Errors []json.RawMessage `json:"errors,omitempty"`
				}
				if err := json.NewDecoder(strings.NewReader(input)).Decode(&errs); err != nil {
					log.Printf("[ERROR] %s\n", err.Error())
				}
				for _, err := range errs.Errors {
					log.Printf("[ERROR] %s\n", err)
				}
				continue
			}
			if app.injectCreatedAt {
				if tweet, ok := data["data"]; ok {
					if tweet, ok := tweet.(map[string]interface{}); ok {
						if createdAt, ok := tweet["created_at"]; ok {
							data["created_at"] = createdAt
						}
					}
				}
			}
			if err := encoder.Encode(data); err != nil {
				log.Printf("[ERROR] %s\n", err)
				continue
			}
		}
	}
}

func (app *App) getStream(ctx context.Context) (<-chan string, error) {
	respCh, err := app.client.GetStream(ctx, app.params)
	if err != nil {
		log.Printf("[ERROR] GetStream failed: %s\n", err.Error())
		//https://developer.twitter.com/en/docs/twitter-api/tweets/filtered-stream/integrate/handling-disconnections
		minInterval := 250 * time.Millisecond
		maxInterval := 16 * time.Second
		switch err := err.(type) {
		case *responseError:
			switch err.StatusCode {
			case http.StatusTooManyRequests:
				minInterval = time.Minute
				maxInterval = 16 * time.Minute
			case http.StatusInternalServerError:
			default:
				return nil, err
			}
		default:
			internalErr := errors.Unwrap(err)
			if internalErr == context.Canceled || internalErr == context.DeadlineExceeded {
				return nil, err
			}
		}
		p := backoff.Exponential(
			backoff.WithMinInterval(minInterval),
			backoff.WithMaxInterval(maxInterval),
			backoff.WithJitterFactor(0.05),
			backoff.WithMultiplier(2.0),
		)
		controller := p.Start(ctx)
		var i int = 0
		for backoff.Continue(controller) {
			i++
			respCh, err = app.client.GetStream(ctx, app.params)
			if err == nil || errors.Unwrap(err) == context.Canceled || errors.Unwrap(err) == context.DeadlineExceeded {
				return respCh, nil
			}
			log.Printf("[ERROR] RetryCount=%d, GetStream failed: %s\n", i, err.Error())
		}
		return nil, err
	}
	return respCh, nil
}
