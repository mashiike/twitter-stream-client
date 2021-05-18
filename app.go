package twstclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	output          io.Writer
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
		output:          config.Output,
		injectCreatedAt: config.InjectCreatedAt,
	}
	return app, nil
}

func (app *App) Run(ctx context.Context) error {
	inputCh := make(chan string, app.workerNum*2)
	tweetCh := make(chan string, app.workerNum*2)
	errCh := make(chan error, app.workerNum*2)
	var wg sync.WaitGroup
	for i := 0; i < app.workerNum; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("[DEBUG] werker_id=%d start\n", workerID)
			app.worker(inputCh, tweetCh, errCh)
			log.Printf("[DEBUG] werker_id=%d end\n", workerID)
		}()
	}
	var rwg sync.WaitGroup
	rwg.Add(1)
	go func() {
		defer rwg.Done()
		log.Println("[DEBUG] start worker error reporter")
		app.errorReporter(errCh)
		log.Println("[DEBUG] end worker error reporter")
	}()
	rwg.Add(1)
	go func() {
		defer rwg.Done()
		log.Println("[DEBUG] start tweet reporter")
		app.tweetReporter(tweetCh)
		log.Println("[DEBUG] end tweet reporter")
	}()
	done := false
	var err error
	for !done {
		log.Println("[INFO] start mainloop")
		err = app.mainLoop(ctx, inputCh)
		log.Println("[INFO] end mainloop")
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
			log.Println("[INFO] 5 sec after try restart mainloop")
			time.Sleep(5 * time.Second)
		}
	}
	close(inputCh)
	wg.Wait()
	close(tweetCh)
	close(errCh)
	rwg.Wait()
	return err
}

func (app *App) mainLoop(ctx context.Context, tweetCh chan<- string) error {
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
				return nil
			}
			if len(resp) <= 0 {
				heartbeatCount++
				log.Println("[DEBUG] heartbeat")
				continue
			}
			tweetCount++
			tweetCh <- resp
		case <-ticker.C:
			log.Printf("[INFO] in 5 Minute, HeartbeatCount=%d, TweetCount=%d", heartbeatCount, tweetCount)
			heartbeatCount = 0
			tweetCount = 0
		}
	}
}

func (app *App) errorReporter(errCh <-chan error) {
	for {
		err, ok := <-errCh
		if ok {
			log.Printf("[INFO] %s", err.Error())
		} else {
			return
		}
	}
}

func (app *App) tweetReporter(tweetCh <-chan string) {
	for {
		tweet, ok := <-tweetCh
		if ok {
			io.WriteString(app.output, tweet)
		} else {
			return
		}
	}
}

type respData struct {
	Data          map[string]interface{} `json:"data,omitempty"`
	MatchingRules interface{}            `json:"matching_rules,omitempty"`
	CreatedAt     interface{}            `json:"created_at,omitempty"`
	Errors        []json.RawMessage      `json:"errors,omitempty"`
}

func (app *App) worker(inputCh <-chan string, tweetCh chan<- string, errCh chan<- error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for {
		input, ok := <-inputCh
		if !ok {
			return
		}
		var data respData
		decoder := json.NewDecoder(strings.NewReader(input))
		if err := decoder.Decode(&data); err != nil {
			errCh <- err
			continue
		}
		if len(data.Errors) > 0 {
			for _, err := range data.Errors {
				errCh <- fmt.Errorf("%s", string(err))
			}
			continue
		}
		if app.injectCreatedAt {
			if createdAt, ok := data.Data["created_at"]; ok {
				data.CreatedAt = createdAt
			}
		}
		if err := encoder.Encode(data); err != nil {
			errCh <- err
			continue
		}
		tweetCh <- buf.String()
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
