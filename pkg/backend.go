package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/grafana/grafana_plugin_model/go/datasource"
	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"golang.org/x/net/context"
)

// ZabbixBackend implements the Grafana backend interface and forwards queries to the ZabbixDatasource
type ZabbixBackend struct {
	plugin.NetRPCUnsupportedPlugin
	logger          hclog.Logger
	datasourceCache *Cache
}

func (b *ZabbixBackend) newZabbixDatasource(hash string) *ZabbixDatasource {
	ds := NewZabbixDatasourceWithHash(b.logger, hash)
	return ds
}

// Query receives requests from the Grafana backend. Requests are filtered by query type and sent to the
// applicable ZabbixDatasource.
func (b *ZabbixBackend) Query(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (resp *datasource.DatasourceResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			err, _ = r.(error)
			b.logger.Error("Fatal error in Zabbix Plugin Backend", "Error", err)
			b.logger.Error(string(debug.Stack()))
			resp = BuildErrorResponse(fmt.Errorf("Unrecoverable error in grafana-zabbix plugin backend"))
		}
	}()

	zabbixDs := b.getCachedDatasource(tsdbReq)

	queryType, err := GetQueryType(tsdbReq)
	if err != nil {
		return nil, err
	}

	switch queryType {
	case "zabbixAPI":
		resp, err = zabbixDs.DirectQuery(ctx, tsdbReq)
	case "query":
		resp, err = zabbixDs.TimeseriesQuery(ctx, tsdbReq)
	case "connectionTest":
		resp, err = zabbixDs.TestConnection(ctx, tsdbReq)
	default:
		err = errors.New("Query not implemented")
		return BuildErrorResponse(err), nil
	}

	if resp == nil && err != nil {
		BuildErrorResponse(fmt.Errorf("Internal error in grafana-zabbix plugin"))
	}
	return
}

func (b *ZabbixBackend) getCachedDatasource(tsdbReq *datasource.DatasourceRequest) *ZabbixDatasource {
	dsInfoHash := HashDatasourceInfo(tsdbReq.GetDatasource())

	if cachedData, ok := b.datasourceCache.Get(dsInfoHash); ok {
		if cachedDS, ok := cachedData.(*ZabbixDatasource); ok {
			return cachedDS
		}
	}

	if b.logger.IsDebug() {
		dsInfo := tsdbReq.GetDatasource()
		b.logger.Debug(fmt.Sprintf("Datasource cache miss (Org %d Id %d '%s' %s)", dsInfo.GetOrgId(), dsInfo.GetId(), dsInfo.GetName(), dsInfoHash))
	}

	ds := b.newZabbixDatasource(dsInfoHash)
	b.datasourceCache.Set(dsInfoHash, ds)
	return ds
}

// GetQueryType determines the query type from a query or list of queries
func GetQueryType(tsdbReq *datasource.DatasourceRequest) (string, error) {
	queryType := "query"
	if len(tsdbReq.Queries) > 0 {
		firstQuery := tsdbReq.Queries[0]
		queryJSON, err := simplejson.NewJson([]byte(firstQuery.ModelJson))
		if err != nil {
			return "", err
		}
		queryType = queryJSON.Get("queryType").MustString("query")
	}
	return queryType, nil
}

// BuildResponse transforms a Zabbix API response to a DatasourceResponse
func BuildResponse(responseData interface{}) (*datasource.DatasourceResponse, error) {
	jsonBytes, err := json.Marshal(responseData)
	if err != nil {
		return nil, err
	}

	return &datasource.DatasourceResponse{
		Results: []*datasource.QueryResult{
			&datasource.QueryResult{
				RefId:    "zabbixAPI",
				MetaJson: string(jsonBytes),
			},
		},
	}, nil
}

// BuildMetricsResponse builds a response object using a given TimeSeries array
func BuildMetricsResponse(results []*datasource.QueryResult) (*datasource.DatasourceResponse, error) {
	return &datasource.DatasourceResponse{
		Results: results,
	}, nil
}

// BuildErrorResponse creates a QueryResult that forwards an error to the front-end
func BuildErrorResponse(err error) *datasource.DatasourceResponse {
	return &datasource.DatasourceResponse{
		Results: []*datasource.QueryResult{
			&datasource.QueryResult{
				RefId: "zabbixAPI",
				Error: err.Error(),
			},
		},
	}
}
