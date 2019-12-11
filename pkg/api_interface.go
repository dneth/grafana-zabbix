package main

import (
	simplejson "github.com/bitly/go-simplejson"
	"github.com/grafana/grafana_plugin_model/go/datasource"
	"golang.org/x/net/context"
)

// ZabbixAPIInterface describes an interface for interacting with the Zabbix aPI
type ZabbixAPIInterface interface {
	// DirectQuery handles query requests to Zabbix
	DirectQuery(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error)
	// TestConnection checks authentication and version of the Zabbix API and returns that info
	TestConnection(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error)
	// RawRequest checks authentication and makes a request to the Zabbix API
	RawRequest(ctx context.Context, dsInfo *datasource.DatasourceInfo, method string, params zabbixParams) (result *simplejson.Json, err error)
	GetFilteredItems(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string, appids []string, itemtype string) (*simplejson.Json, error)
	GetAppsByHostIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string) (*simplejson.Json, error)
	GetHostsByGroupIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupids []string) (*simplejson.Json, error)
	GetAllGroups(ctx context.Context, dsInfo *datasource.DatasourceInfo) (*simplejson.Json, error)
	GetHistoryOrTrend(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items []*simplejson.Json, useTrend bool) ([]*simplejson.Json, error)
}
