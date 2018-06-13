package cache

import (
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/panjf2000/goproxy/config"
	"github.com/panjf2000/goproxy/handlers"
	"github.com/valyala/fasthttp"
)

// HttpCache is the entity of http cache.
type HttpCache struct {
	Header       *fasthttp.ResponseHeader `json:"header"`
	Body         io.Writer                  `json:"body"`
	StatusCode   int                     `json:"status_code"`
	URI          string                  `json:"url"`
	LastModified string                  `json:"last_modified"` //eg:"Fri, 27 Jun 2014 07:19:49 GMT"
	ETag         string                  `json:"etag"`
	Mustverified bool                    `json:"must_verified"`
	//Vlidity is a time when to verfiy the cache again.
	Vlidity time.Time `json:"vlidity"`
	maxAge  int64     `json:"-"`
}

// NewCacheResp create a new HttpCache instance.
func NewCacheResp(resp *fasthttp.Response) *HttpCache {
	c := new(HttpCache)
	c.Header = new(fasthttp.ResponseHeader)
	handlers.CopyHeaders(c.Header, &resp.Header)
	c.StatusCode = resp.Header.StatusCode()

	var err error
	c.Body = resp.BodyWriter()

	if c.Header == nil {
		return nil
	}

	c.ETag = string(c.Header.Peek("ETag"))
	c.LastModified = string(c.Header.Peek("Last-Modified"))

	cacheControl := string(c.Header.Peek("Cache-Control"))

	// no-cache means you should verify data before use cache.
	// only use cache when remote server returns 302 status.
	if strings.Index(cacheControl, "no-cache") != -1 ||
		strings.Index(cacheControl, "must-revalidate") != -1 ||
		strings.Index(cacheControl, "proxy-revalidate") != -1 {
		c.Mustverified = false
		return nil
	}
	c.Mustverified = true

	if Expires := string(c.Header.Peek("Expires")); Expires != "" {
		c.Vlidity, err = time.Parse(http.TimeFormat, Expires)
		if err != nil {
			return nil
		}
		log.Println("expire:", c.Vlidity)
	}

	maxAge := getAge(cacheControl)
	if maxAge != -1 {
		var Time time.Time
		date := string(c.Header.Peek("Date"))
		if date == "" {
			Time = time.Now().UTC()
		} else {
			Time, err = time.Parse(time.RFC1123, date)
			if err != nil {
				return nil
			}
		}
		c.Vlidity = Time.Add(time.Duration(maxAge) * time.Second)
		c.maxAge = maxAge
	} else {
		//c.maxAge, max_age = 0.1 * 60 * 60, 0.1 * 60 * 60
		cacheTimeout := config.RuntimeViper.GetInt64("server.cache_timeout")
		c.maxAge, maxAge = cacheTimeout, cacheTimeout
		Time := time.Now().UTC()
		c.Vlidity = Time.Add(time.Duration(maxAge) * time.Second)
	}
	log.Println("all:", c.Vlidity)

	return c
}

// Verify verifies whether cache is out of date.
func (c *HttpCache) Verify() bool {
	if c.Mustverified == true && c.Vlidity.After(time.Now().UTC()) {
		return true
	}

	newReq, err := http.NewRequest("GET", c.URI, nil)
	if err != nil {
		return false
	}

	if c.LastModified != "" {
		newReq.Header.Add("If-Modified-Since", c.LastModified)
	}
	if c.ETag != "" {
		newReq.Header.Add("If-None-Match", c.ETag)
	}
	Tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	resp, err := Tr.RoundTrip(newReq)
	if err != nil {
		return false
	}

	if resp.StatusCode != http.StatusNotModified {
		return false
	}
	return false
}

// WriteTo write response into HttpCache.
func (c *HttpCache) WriteTo(resp *fasthttp.Response) (int64, error) {

	handlers.CopyHeaders(&resp.Header, c.Header)

	return resp.WriteTo(c.Body)
	//return resp.Write(c.Body)

}


// getAge from Cache Control get cache's lifetime.
func getAge(cacheControl string) (age int64) {
	f := func(sage string) int64 {
		var tmpAge int64
		idx := strings.Index(cacheControl, sage)
		if idx != -1 {
			for i := idx + len(sage) + 1; i < len(cacheControl); i++ {
				if cacheControl[i] >= '0' && cacheControl[i] <= '9' {
					tmpAge = tmpAge*10 + int64(cacheControl[i])
				} else {
					break
				}
			}
			return tmpAge
		}
		return -1
	}
	if sMaxage := f("s-maxage"); sMaxage != -1 {
		return sMaxage
	}
	return f("max-age")
}
