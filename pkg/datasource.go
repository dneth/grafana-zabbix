package main

import (
	"errors"
	"regexp"
	"time"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/grafana/grafana_plugin_model/go/datasource"
	hclog "github.com/hashicorp/go-hclog"
	"golang.org/x/net/context"
)

// ZabbixDatasource stores state about a specific datasource and provides methods to make
// requests to the Zabbix API
type ZabbixDatasource struct {
	client ZabbixAPIInterface
	logger hclog.Logger
	hash   string
}

// NewZabbixDatasource returns an instance of ZabbixDatasource with an API Client
func NewZabbixDatasource() *ZabbixDatasource {
	return &ZabbixDatasource{
		client: NewZabbixAPIClient(),
	}
}

// NewZabbixDatasourceWithHash returns an instance of ZabbixDatasource with an API Client and the given identifying hash
func NewZabbixDatasourceWithHash(hash string) *ZabbixDatasource {
	return &ZabbixDatasource{
		client: NewZabbixAPIClient(),
		hash:   hash,
	}
}

type categories struct {
	Transform []map[string]interface{}
	Aggregate []map[string]interface{}
	Filter    []map[string]interface{}
	Trends    []map[string]interface{}
	Time      []map[string]interface{}
	Alias     []map[string]interface{}
	Special   []map[string]interface{}
}

func (ds *ZabbixDatasource) queryNumericItems(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	jsonQueries := make([]*simplejson.Json, 0)
	for _, query := range tsdbReq.Queries {
		json, err := simplejson.NewJson([]byte(query.ModelJson))
		if err != nil {
			return nil, err
		}

		jsonQueries = append(jsonQueries, json)
	}

	if len(jsonQueries) == 0 {
		return nil, errors.New("At least one query should be provided")
	}

	items, err := ds.getItemsFromTarget(ctx, tsdbReq.GetDatasource(), jsonQueries)
	if err != nil {
		return nil, err
	}

	response, err := ds.queryNumericDataForItems(ctx, tsdbReq, items, jsonQueries, isUseTrend(tsdbReq.GetTimeRange()))
	if err != nil {
		return nil, err
	}

	return BuildResponse(response)
}

func (ds *ZabbixDatasource) getItemsFromTarget(ctx context.Context, dsInfo *datasource.DatasourceInfo, jsonQueries []*simplejson.Json) ([]*simplejson.Json, error) {
	jsonQuery := jsonQueries[0].Get("target")
	groupFilter := jsonQuery.GetPath("group", "filter").MustString()
	hostFilter := jsonQuery.GetPath("host", "filter").MustString()
	appFilter := jsonQuery.GetPath("application", "filter").MustString()
	itemFilter := jsonQuery.GetPath("item", "filter").MustString()

	apps, hostids, err := ds.getApps(ctx, dsInfo, groupFilter, hostFilter, appFilter)
	if err != nil {
		return nil, err
	}
	var appids []string
	for i := range apps {
		appids = append(appids, apps[i].Get("applicationid").MustString())
	}

	var allItems *simplejson.Json
	if len(hostids) > 0 {
		allItems, err = ds.client.GetFilteredItems(ctx, dsInfo, hostids, nil, "num")
	} else if len(appids) > 0 {
		allItems, err = ds.client.GetFilteredItems(ctx, dsInfo, nil, appids, "num")
	}

	if err != nil {
		return nil, err
	}
	var items []*simplejson.Json
	for k := range allItems.Get("result").MustArray() {
		if allItems.Get("result").Get("status").MustString() == "0" {
			matched, err := regexp.MatchString(itemFilter, allItems.Get("result").GetIndex(k).MustString())
			if err != nil {
				return nil, err
			} else if matched {
				items = append(items, allItems.Get("result").GetIndex(k))
			}
		}
	}
	return items, nil
}

func (ds *ZabbixDatasource) getApps(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string, hostFilter string, appFilter string) (result []*simplejson.Json, filteredHostids []string, err error) {
	hosts, err := ds.getHosts(ctx, dsInfo, groupFilter, hostFilter)
	if err != nil {
		return nil, nil, err
	}
	var hostids []string
	for i := range hosts {
		hostids = append(hostids, hosts[i].Get("hostid").MustString())
	}
	allApps, err := ds.client.GetAppsByHostIDs(ctx, dsInfo, hostids)
	if err != nil {
		return nil, hostids, err
	}
	var apps []*simplejson.Json
	for k := range allApps.Get("result").MustArray() {
		matched, err := regexp.MatchString(appFilter, allApps.Get("result").GetIndex(k).MustString())
		if err != nil {
			return nil, hostids, err
		} else if matched {
			apps = append(apps, allApps.Get("result").GetIndex(k))
		}
	}
	return apps, hostids, nil
}

func (ds *ZabbixDatasource) getHosts(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string, hostFilter string) ([]*simplejson.Json, error) {
	groups, err := ds.getGroups(ctx, dsInfo, groupFilter)
	if err != nil {
		return nil, err
	}
	var groupids []string
	for i := range groups {
		groupids = append(groupids, groups[i].Get("groupid").MustString())
	}
	allHosts, err := ds.client.GetHostsByGroupIDs(ctx, dsInfo, groupids)
	if err != nil {
		return nil, err
	}
	var hosts []*simplejson.Json
	for k := range allHosts.Get("result").MustArray() {
		matched, err := regexp.MatchString(hostFilter, allHosts.Get("result").GetIndex(k).MustString())
		if err != nil {
			return nil, err
		} else if matched {
			hosts = append(hosts, allHosts.Get("result").GetIndex(k))
		}
	}
	return hosts, nil
}

func (ds *ZabbixDatasource) getGroups(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string) ([]*simplejson.Json, error) {
	allGroups, err := ds.client.GetAllGroups(ctx, dsInfo)
	if err != nil {
		return nil, err
	}
	var groups []*simplejson.Json
	for k := range allGroups.Get("result").MustArray() {
		matched, err := regexp.MatchString(groupFilter, allGroups.Get("result").GetIndex(k).MustString())
		if err != nil {
			return nil, err
		} else if matched {
			groups = append(groups, allGroups.Get("result").GetIndex(k))
		}
	}
	return groups, nil
}

func (ds *ZabbixDatasource) queryNumericDataForItems(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items []*simplejson.Json, jsonQueries []*simplejson.Json, useTrend bool) (*simplejson.Json, error) {
	valueType := ds.getTrendValueType(jsonQueries)
	consolidateBy := ds.getConsolidateBy(jsonQueries)
	if consolidateBy == "" {
		consolidateBy = valueType
	}
	ds.logger.Info(consolidateBy)

	history, err := ds.client.GetHistoryOrTrend(ctx, tsdbReq, items, useTrend)
	if err != nil {
		return nil, err
	}
	return convertHistory(history, items)
}
func (ds *ZabbixDatasource) getTrendValueType(jsonQueries []*simplejson.Json) string {
	var trendFunctions []string
	var trendValueFunc string

	for _, j := range new(categories).Trends {
		trendFunctions = append(trendFunctions, j["name"].(string))
	}

	for i := range jsonQueries[0].Get("target").MustArray() {
		for _, j := range trendFunctions {
			if j == jsonQueries[0].Get("target").GetIndex(i).GetPath("function", "def", "name").MustString() {
				trendValueFunc = j
			}
		}
	}

	if trendValueFunc == "" {
		trendValueFunc = "avg"
	}

	return trendValueFunc
}

func (ds *ZabbixDatasource) getConsolidateBy(jsonQueries []*simplejson.Json) string {
	var consolidateBy []string
	for i, j := range jsonQueries[0].Get("target").MustArray() {
		if jsonQueries[0].Get("target").GetIndex(i).GetPath("function", "def", "name").MustString() == "consolidateBy" {
			consolidateBy = append(consolidateBy, j.(string))
		}
	}
	return consolidateBy[0]
}

func isUseTrend(timeRange *datasource.TimeRange) bool {
	if (timeRange.GetFromEpochMs() < 7*24*time.Hour.Nanoseconds()/1000000) ||
		(timeRange.GetFromEpochMs()-timeRange.GetToEpochMs() > 4*24*time.Hour.Nanoseconds()/1000000) {
		return true
	}
	return false
}

func convertHistory(history []*simplejson.Json, items []*simplejson.Json) (*simplejson.Json, error) {
	groupedHistory := map[string][]*simplejson.Json{}
	hosts := map[string][]*simplejson.Json{}
	params, err := simplejson.NewJson([]byte(``))
	if err != nil {
		return nil, err
	}

	for _, i := range history {
		groupedHistory[i.Get("itemid").MustString()] = append(groupedHistory[i.Get("itemid").MustString()], i)
	}

	for _, j := range items {
		hosts[j.Get("hostid").MustString()] = append(hosts[j.Get("hostid").MustString()], j.Get("hosts"))
	}

	for _, k := range groupedHistory {
		item := k[0].Get("item")
		alias := item.Get("name").MustString()
		for l, m := range hosts {
			if l == item.Get("hostid").MustString() {
				alias += m[0].Get("name").MustString()
			}
		}
		params.Set(alias, k)
	}
	return params, nil
}
