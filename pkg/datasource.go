package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/alexanderzobnin/grafana-zabbix/pkg/zabbix"
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

// DirectQuery handles query requests to Zabbix
func (ds *ZabbixDatasource) DirectQuery(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	// result, queryExistInCache := ds.queryCache.Get(HashString(tsdbReq.String()))

	// if queryExistInCache {
	// 	return BuildResponse(result)
	// }

	dsInfo := tsdbReq.GetDatasource()

	queries := []requestModel{}
	for _, query := range tsdbReq.Queries {
		request := requestModel{}
		err := json.Unmarshal([]byte(query.GetModelJson()), &request)

		if err != nil {
			return nil, err
		}

		ds.logger.Debug("ZabbixAPIQuery", "method", request.Target.Method, "params", request.Target.Params)

		queries = append(queries, request)
	}

	if len(queries) == 0 {
		return nil, errors.New("At least one query should be provided")
	}

	query := queries[0]

	response, err := ds.client.RawRequest(ctx, dsInfo, query.Target.Method, query.Target.Params)
	// ds.queryCache.Set(HashString(tsdbReq.String()), response)
	if err != nil {
		newErr := fmt.Errorf("Error in direct query: %w", err)
		ds.logger.Error(newErr.Error())
		return nil, newErr
	}

	return BuildResponse(response)
}

// TestConnection checks authentication and version of the Zabbix API and returns that info
func (ds *ZabbixDatasource) TestConnection(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	dsInfo := tsdbReq.GetDatasource()

	result, err := ds.client.RawRequest(ctx, dsInfo, "apiinfo.version", zabbixParams{})
	if err != nil {
		ds.logger.Debug("TestConnection", "error", err)
		return BuildErrorResponse(fmt.Errorf("Version check failed: %w", err)), nil
	}

	ds.logger.Debug("TestConnection", "result", string(result))

	var version string
	err = json.Unmarshal(result, &version)
	if err != nil {
		ds.logger.Error("Internal error while parsing response from Zabbix", err.Error())
		return nil, fmt.Errorf("Internal error while parsing response from Zabbix")
	}

	testResponse := connectionTestResponse{
		ZabbixVersion: version,
	}

	return BuildResponse(testResponse)
}

func (ds *ZabbixDatasource) queryNumericItems(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	tStart := time.Now()
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

	firstQuery := jsonQueries[0]

	groupFilter := firstQuery.GetPath("group", "filter").MustString()
	hostFilter := firstQuery.GetPath("host", "filter").MustString()
	appFilter := firstQuery.GetPath("application", "filter").MustString()
	itemFilter := firstQuery.GetPath("item", "filter").MustString()

	ds.logger.Debug("queryNumericItems",
		"func", "ds.getItems",
		"groupFilter", groupFilter,
		"hostFilter", hostFilter,
		"appFilter", appFilter,
		"itemFilter", itemFilter)

	items, err := ds.getItems(ctx, tsdbReq.GetDatasource(), groupFilter, hostFilter, appFilter, itemFilter, "num")
	if err != nil {
		return nil, err
	}
	ds.logger.Debug("queryNumericItems", "finished", "ds.getItems", "timeElapsed", time.Now().Sub(tStart))

	metrics, err := ds.queryNumericDataForItems(ctx, tsdbReq, items, jsonQueries, isUseTrend(tsdbReq.GetTimeRange()))
	if err != nil {
		return nil, err
	}
	ds.logger.Debug("queryNumericItems", "finished", "queryNumericDataForItems", "timeElapsed", time.Now().Sub(tStart))

	return BuildMetricsResponse(metrics)
}

func (ds *ZabbixDatasource) getItems(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string, hostFilter string, appFilter string, itemFilter string, itemType string) (zabbix.Items, error) {
	tStart := time.Now()

	hosts, err := ds.getHosts(ctx, dsInfo, groupFilter, hostFilter)
	if err != nil {
		return nil, err
	}

	var hostids []string
	for _, host := range hosts {
		hostids = append(hostids, host.ID)
	}
	ds.logger.Debug("getItems", "finished", "getHosts", "timeElapsed", time.Now().Sub(tStart))

	var items zabbix.Items
	// TODO: This condition doesn't seem right
	if len(hostids) > 0 {
		items, err = ds.client.GetFilteredItems(ctx, dsInfo, hostids, nil, "num")
		if err != nil {
			return nil, err
		}
	} else {
		apps, err := ds.getApps(ctx, dsInfo, hostids, appFilter)
		if err != nil {
			return nil, err
		}
		var appids []string
		for _, app := range apps {
			appids = append(appids, app.ID)
		}
		ds.logger.Debug("getItems", "finished", "getApps", "timeElapsed", time.Now().Sub(tStart))

		items, err = ds.client.GetFilteredItems(ctx, dsInfo, nil, appids, "num")
		if err != nil {
			return nil, err
		}
	}
	ds.logger.Debug("getItems", "finished", "getAllItems", "timeElapsed", time.Now().Sub(tStart))

	filteredItems := zabbix.Items{}
	for _, item := range items {
		if item.Status == "0" {
			matched, err := regexp.MatchString(itemFilter, item.Name)
			if err != nil {
				ds.logger.Warn(fmt.Errorf("RegExp failed: %w", err).Error())
			} else if matched {
				filteredItems = append(filteredItems, item)
			}
		}
	}

	ds.logger.Debug("getItems", "found", len(items), "matches", len(filteredItems))
	ds.logger.Debug("getItems", "totalTimeTaken", time.Now().Sub(tStart))
	return filteredItems, nil
}

func (ds *ZabbixDatasource) getApps(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string, appFilter string) (zabbix.Applications, error) {
	apps, err := ds.client.GetAppsByHostIDs(ctx, dsInfo, hostids)
	if err != nil {
		return nil, err
	}

	filteredApps := zabbix.Applications{}
	for _, app := range apps {
		// ds.logger.Info(fmt.Sprintf("App: %+v", app))
		matched, err := regexp.MatchString(appFilter, app.Name)
		if err != nil {
			return nil, err
		} else if matched {
			filteredApps = append(filteredApps, app)
		}
	}
	ds.logger.Debug("getapps", "found", len(apps), "matches", len(filteredApps))
	return filteredApps, nil
}

func (ds *ZabbixDatasource) getHosts(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string, hostFilter string) (zabbix.Hosts, error) {
	groups, err := ds.getGroups(ctx, dsInfo, groupFilter)
	if err != nil {
		return nil, err
	}

	var groupids []string
	for _, group := range groups {
		groupids = append(groupids, group.ID)
	}

	hosts, err := ds.client.GetHostsByGroupIDs(ctx, dsInfo, groupids)
	if err != nil {
		return nil, err
	}

	filteredHosts := zabbix.Hosts{}
	for _, host := range hosts {
		matched, err := regexp.MatchString(hostFilter, host.Name)
		if err != nil {
			return nil, err
		} else if matched {
			filteredHosts = append(filteredHosts, host)
		}
	}

	ds.logger.Debug("getHosts", "found", len(hosts), "matches", len(filteredHosts))
	return filteredHosts, nil
}

func (ds *ZabbixDatasource) getGroups(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupFilter string) (zabbix.Groups, error) {
	groups, err := ds.client.GetAllGroups(ctx, dsInfo)
	if err != nil {
		return nil, err
	}
	filteredGroups := zabbix.Groups{}
	for _, group := range groups {
		matched, err := regexp.MatchString(groupFilter, group.Name)
		if err != nil {
			return nil, err
		} else if matched {
			filteredGroups = append(filteredGroups, group)
		}
	}

	ds.logger.Debug("getGroups", "found", len(groups), "matches", len(filteredGroups))
	return filteredGroups, nil
}

func (ds *ZabbixDatasource) queryNumericDataForItems(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items zabbix.Items, jsonQueries []*simplejson.Json, useTrend bool) ([]*datasource.TimeSeries, error) {
	valueType := ds.getTrendValueType(jsonQueries)
	consolidateBy := ds.getConsolidateBy(jsonQueries)
	if consolidateBy == "" {
		consolidateBy = valueType
	}
	ds.logger.Info(consolidateBy)

	var timeSeries []*datasource.TimeSeries
	if useTrend {
		trend, err := ds.client.GetTrend(ctx, tsdbReq, items)
		if err != nil {
			return nil, err
		}
		timeSeries = convertTrend(trend, items, valueType)
	} else {
		history, err := ds.client.GetHistory(ctx, tsdbReq, items)
		if err != nil {
			return nil, err
		}
		timeSeries = convertHistory(history, items)
	}

	return timeSeries, nil
}

func (ds *ZabbixDatasource) getTrendValueType(jsonQueries []*simplejson.Json) string {
	var trendFunctions []string
	var trendValueFunc string

	// TODO: loop over actual returned categories
	for _, j := range new(categories).Trends {
		trendFunctions = append(trendFunctions, j["name"].(string))
	}
	for _, k := range jsonQueries[0].Get("functions").MustArray() {
		for _, j := range trendFunctions {
			if j == k.(map[string]interface{})["def"].(map[string]interface{})["name"] {
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
	var consolidateBy string

	for _, k := range jsonQueries[0].Get("functions").MustArray() {
		if k.(map[string]interface{})["def"].(map[string]interface{})["name"] == "consolidateBy" {
			defParams := k.(map[string]interface{})["def"].(map[string]interface{})["params"].([]interface{})
			if len(defParams) > 0 {
				consolidateBy = defParams[0].(string)
			}
		}
	}
	return consolidateBy
}

func isUseTrend(timeRange *datasource.TimeRange) bool {
	fromSec := timeRange.GetFromEpochMs() / 1000
	toSec := timeRange.GetToEpochMs() / 1000
	if (fromSec < time.Now().Add(time.Hour*-7*24).Unix()) ||
		(toSec-fromSec > (4 * 24 * time.Hour).Milliseconds()) {
		return true
	}
	return false
}

func convertHistory(history zabbix.History, items zabbix.Items) []*datasource.TimeSeries {
	seriesMap := map[string]*datasource.TimeSeries{}

	for _, item := range items {
		seriesMap[item.ID] = &datasource.TimeSeries{
			Name:   fmt.Sprintf("%s %s", item.Hosts[0].Name, item.Name),
			Points: []*datasource.Point{},
		}
	}

	for _, point := range history {
		seriesMap[point.ItemID].Points = append(seriesMap[point.ItemID].Points, &datasource.Point{
			Timestamp: point.Clock*1000 + int64(math.Round(float64(point.NS)/1000000)),
			Value:     point.Value,
		})
	}

	seriesList := []*datasource.TimeSeries{}
	for _, series := range seriesMap {
		seriesList = append(seriesList, series)
	}
	return seriesList
}

func convertTrend(history zabbix.Trend, items zabbix.Items, trendValueType string) []*datasource.TimeSeries {
	var trendValueFunc func(zabbix.TrendPoint) float64
	switch trendValueType {
	case "min":
		trendValueFunc = func(tp zabbix.TrendPoint) float64 { return tp.ValueMin }
	case "avg":
		trendValueFunc = func(tp zabbix.TrendPoint) float64 { return tp.ValueAvg }
	case "max":
		trendValueFunc = func(tp zabbix.TrendPoint) float64 { return tp.ValueMax }
	}

	seriesMap := map[string]*datasource.TimeSeries{}

	for _, item := range items {
		seriesMap[item.ID] = &datasource.TimeSeries{
			Name:   fmt.Sprintf("%s %s", item.Hosts[0].Name, item.Name),
			Points: []*datasource.Point{},
		}
	}

	for _, point := range history {
		seriesMap[point.ItemID].Points = append(seriesMap[point.ItemID].Points, &datasource.Point{
			Timestamp: point.Clock * 1000,
			Value:     trendValueFunc(point),
		})

	}

	seriesList := []*datasource.TimeSeries{}
	for _, series := range seriesMap {
		seriesList = append(seriesList, series)
	}
	return seriesList
}
