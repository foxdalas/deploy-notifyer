package elastic

import (
	"context"
	"encoding/json"
	"errors"
	elastic "github.com/olivere/elastic/v7"
	"net/http"
	"syscall"
	"time"
)

const (
	layoutISO = "2006.01.02"
)

func New(elasticHost []string, index string, kibanaIndex string, yesterday time.Time) (*elasticSearch, error) {
	client, err := elastic.NewClient(
		elastic.SetURL(elasticHost...),
		elastic.SetSniff(false),
		elastic.SetRetrier(NewEsRetrier()),
		elastic.SetHealthcheck(true),
		elastic.SetHealthcheckTimeout(time.Second*300),
	)
	if err != nil {
		return nil, err
	}

	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)

	return &elasticSearch{
		Client:      client,
		Ctx:         ctx,
		Index:       index,
		KibanaIndex: kibanaIndex,
		Yesterday:   yesterday,
	}, nil
}

func NewEsRetrier() *EsRetrier {
	return &EsRetrier{
		backoff: elastic.NewExponentialBackoff(10*time.Millisecond, 8*time.Second),
	}
}

func (r *EsRetrier) Retry(ctx context.Context, retry int, req *http.Request, resp *http.Response, err error) (time.Duration, bool, error) {
	if err == syscall.ECONNREFUSED {
		return 0, false, errors.New("Elasticsearch or network down")
	}

	if retry >= 5 {
		return 0, false, nil
	}

	wait, stop := r.backoff.Next(retry)
	return wait, stop, nil
}

func (e *elasticSearch) getTotalCount() (int64, error) {
	query := elastic.NewBoolQuery()
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "Unknown"))
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "ci"))
	return e.Client.Count(e.Index + "-" + e.Yesterday.Format(layoutISO)).Query(query).Do(e.Ctx)
}

func (e *elasticSearch) afterhoursDeploys() (*elastic.SearchResult, int64, error) {
	evening := time.Date(e.Yesterday.Year(), e.Yesterday.Month(), e.Yesterday.Day(), 14, 00, 00, 0, time.UTC)
	//morning := time.Date(e.Yesterday.Year(), e.Yesterday.Month(), e.Yesterday.Day(), 5, 30, 00, 0, time.UTC)
	query := elastic.NewBoolQuery()
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "Unknown"))
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "ci"))
	query = query.Must(elastic.NewTermQuery("production.keyword", "true"))
	query = query.Filter(elastic.NewRangeQuery("@timestamp").Gte(evening.Format(time.RFC3339)).Lte(time.Now().UTC().Format(time.RFC3339)).TimeZone("UTC"))
	searchResult, err := e.Client.Search().
		Index(e.Index+"-"+e.Yesterday.Format(layoutISO)). // search in index
		Query(query).                                     // specify the query
		Size(100).
		Pretty(true).
		Sort("@timestamp", true).
		IgnoreUnavailable(true).
		Do(e.Ctx)

	total := int64(0)
	if err == nil {
		total = searchResult.TotalHits()
	}
	return searchResult, total, err
}

func (e *elasticSearch) beforeDeploys() (*elastic.SearchResult, int64, error) {
	start := time.Date(e.Yesterday.Year(), e.Yesterday.Month(), e.Yesterday.Day(), 0, 0, 00, 0, time.UTC)
	morning := time.Date(e.Yesterday.Year(), e.Yesterday.Month(), e.Yesterday.Day(), 5, 0, 00, 0, time.UTC)
	query := elastic.NewBoolQuery()
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "Unknown"))
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "ci"))
	query = query.Must(elastic.NewTermQuery("production.keyword", "true"))
	query = query.Filter(elastic.NewRangeQuery("@timestamp").Gte(start.Format(time.RFC3339)).Lte(morning.Format(time.RFC3339)).TimeZone("UTC"))
	searchResult, err := e.Client.Search().
		Index(e.Index+"-"+time.Now().Format(layoutISO)). // search in index
		Query(query).                                    // specify the query
		Size(100).
		Pretty(true).
		Sort("@timestamp", true).
		IgnoreUnavailable(true).
		Do(e.Ctx)

	total := int64(0)
	if err == nil {
		total = searchResult.TotalHits()
	}
	return searchResult, total, err
}

func (e *elasticSearch) getEnvDeploys(isProduction bool) (*elastic.SearchResult, int64, error) {
	aggregationName := "user"
	subAggr := elastic.NewTermsAggregation().Field("state.keyword")
	aggr := elastic.NewTermsAggregation().Field("user.keyword").SubAggregation("state", subAggr).Size(3)
	query := elastic.NewBoolQuery()
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "Unknown"))
	query = query.MustNot(elastic.NewTermQuery("user.keyword", "ci"))
	if isProduction {
		query.Must(
			elastic.NewTermQuery("production", "true"),
		)
	} else {
		query.MustNot(
			elastic.NewTermQuery("production", "true"),
		)
	}
	return e.searchResults(query, aggr, aggregationName, e.Yesterday.Format(layoutISO))
}

func (e *elasticSearch) GetDeploys(ctx context.Context, elasticClient *elastic.Client) (Stats, error) {
	var stats Stats
	count, err := e.getTotalCount()
	if err != nil {
		return stats, err
	}
	stats.Total = count

	//Production data
	search, count, err := e.getEnvDeploys(true)
	if err != nil {
		return stats, err
	}
	stats = e.productionAggregation(search, stats)
	stats.Production.Total = count

	//Development data
	search, count, err = e.getEnvDeploys(false)
	if err != nil {
		return stats, err
	}
	stats = e.developmentAggregation(search, stats)
	stats.Development.Total = count

	search, _, _ = e.afterhoursDeploys()

	for _, t := range search.Hits.Hits {
		var doc Document
		err := json.Unmarshal(t.Source, &doc)
		if err != nil {
			return stats, err
		}
		stats.Production.AfterWork = append(stats.Production.AfterWork, doc)

	}

	search, _, err = e.beforeDeploys()
	if err != nil {
		return stats, err
	}
	for _, t := range search.Hits.Hits {
		var doc Document
		err := json.Unmarshal(t.Source, &doc)
		if err != nil {
			return stats, err
		}
		stats.Production.AfterWork = append(stats.Production.AfterWork, doc)

	}

	return stats, err
}

func (e *elasticSearch) productionAggregation(searchResult *elastic.SearchResult, stats Stats) Stats {
	env, found := searchResult.Aggregations.Terms("user")
	if found {
		for _, e := range env.Buckets {
			result := &User{
				Name:    e.Key.(string),
				Count:   int32(e.DocCount),
				Success: 0,
				Fail:    0,
			}
			if subAgg, found := e.Aggregations.Terms("state"); found {
				for _, subBucket := range subAgg.Buckets {
					if subBucket.Key.(string) == "successful" {
						result.Success = int32(subBucket.DocCount)
					}
					if subBucket.Key.(string) == "fail" {
						result.Success = int32(subBucket.DocCount)
					}
				}
			}
			stats.Production.Users = append(stats.Production.Users, result)
		}
	}
	return stats
}

func (e *elasticSearch) developmentAggregation(searchResult *elastic.SearchResult, stats Stats) Stats {
	env, found := searchResult.Aggregations.Terms("user")
	if found {
		for _, e := range env.Buckets {
			result := &User{
				Name:    e.Key.(string),
				Count:   int32(e.DocCount),
				Success: 0,
				Fail:    0,
			}
			if subAgg, found := e.Aggregations.Terms("state"); found {
				for _, subBucket := range subAgg.Buckets {
					if subBucket.Key.(string) == "successful" {
						result.Success = int32(subBucket.DocCount)
					}
					if subBucket.Key.(string) == "fail" {
						result.Success = int32(subBucket.DocCount)
					}
				}
			}
			stats.Development.Users = append(stats.Development.Users, result)
		}
	}
	return stats
}

func (e *elasticSearch) searchResults(query *elastic.BoolQuery, aggregationString *elastic.TermsAggregation, aggregationName string, date string) (*elastic.SearchResult, int64, error) {
	searchResult, err := e.Client.Search().
		Index(e.Index+"-"+date). // search in index
		Query(query).            // specify the query
		Size(0).
		Aggregation(aggregationName, aggregationString).
		Pretty(true).
		AllowNoIndices(true).
		IgnoreUnavailable(true).
		Do(e.Ctx)
	count, err := e.Client.Count(e.Index + "-" + e.Yesterday.Format(layoutISO)).Query(query).Do(e.Ctx)
	return searchResult, count, err
}
