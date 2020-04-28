package elastic

import (
	"github.com/olivere/elastic/v7"
	"golang.org/x/net/context"
	"time"
)

type elasticSearch struct {
	Ctx         context.Context
	Client      *elastic.Client
	Index       string
	KibanaIndex string
	Yesterday   time.Time
}

type EsRetrier struct {
	backoff elastic.Backoff
}

type Stats struct {
	Total       int64
	Production  Production
	Development Development
}

type Production struct {
	Total     int64
	Users     []*User
	AfterWork []Document
}

type Development struct {
	Total int64
	Users []*User
}

type User struct {
	Name    string
	Count   int32
	Success int32
	Fail    int32
}

type Document struct {
	Timestamp  time.Time `json:"@timestamp"`
	FinishTime time.Time `json:"finish_timestamp"`
	User       string    `json:"user"`
	Msg        string    `json:"msg"`
	Tags       string    `json:"tags"`
	Build      string    `json:"build"`
	Datacenter string    `json:"datacenter"`
	Annotags   string    `json:"annotags"`
	Apps       []string  `json:"apps"`
	Production string    `json:"production"`
	State      string    `json:"state"`
	Namespace  string    `json:"namespace"`
}
