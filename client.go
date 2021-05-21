package twstclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

type Client struct {
	Endpoint    string
	BearerToken string
	HTTPClient  *http.Client
}

func NewClient(config *Config) (*Client, error) {
	u, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = "/2/tweets/search/stream"
	c := &Client{
		Endpoint:    u.String(),
		BearerToken: config.BearerToken,
		HTTPClient: &http.Client{
			Timeout: 0, //No timeout due to long polling
		},
	}
	return c, nil
}

func (c *Client) GetStream(ctx context.Context, params url.Values) (<-chan string, error) {
	query := params.Encode()
	endpoint := c.Endpoint + "?" + query
	log.Printf("[DEBUG] GET %s", endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.BearerToken))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var err responseError
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&err); err != nil {
			log.Printf("[ERROR] response parse failed:%s", err.Error())
		}
		err.StatusCode = resp.StatusCode
		resp.Body.Close()
		return nil, &err
	}
	respCh := newResponseChannel(resp.Body, 10)
	return respCh, nil
}

const (
	maxScanTokenSize = 256 * 1024
	startBufSize     = 4096
)

func newResponseChannel(body io.ReadCloser, bufferSize int) <-chan string {
	respCh := make(chan string, bufferSize)
	go func() {
		defer body.Close()
		scanner := bufio.NewScanner(body)
		buf := make([]byte, startBufSize)
		scanner.Buffer(buf, maxScanTokenSize)
		log.Println("[INFO] start response scan")
		for scanner.Scan() {
			str := scanner.Text()
			log.Printf("[DEBUG] receive `%s`\n", str)
			respCh <- str
		}
		log.Println("[INFO] end response scan")
		close(respCh)
	}()
	return respCh
}

type responseError struct {
	ClientID   string `json:"client_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Type       string `json:"type,omitempty"`
	Reason     string `json:"reason,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

func (err *responseError) Error() string {
	bs, e := json.Marshal(err)
	if e != nil {
		log.Printf("[ERROR] response error mashal failed: %s\n", e.Error())
		return fmt.Sprintf("Status=%d Title=%s Type=%s Detail=%s", err.StatusCode, err.Title, err.Type, err.Detail)
	}
	return string(bs)
}
