package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/alexanderzobnin/grafana-zabbix/pkg/zabbix"
	simplejson "github.com/bitly/go-simplejson"
	"github.com/grafana/grafana_plugin_model/go/datasource"
	hclog "github.com/hashicorp/go-hclog"
	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

// ZabbixAPIClient stores state about a specific datasource and provides methods to make
// requests to the Zabbix API
type ZabbixAPIClient struct {
	queryCache *Cache
	logger     hclog.Logger
	httpClient *http.Client
	authToken  string
}

// NewZabbixAPIClient returns an initialized ZabbixDatasource
func NewZabbixAPIClient() *ZabbixAPIClient {
	return &ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					Renegotiation: tls.RenegotiateFreelyAsClient,
				},
				Proxy: http.ProxyFromEnvironment,
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).Dial,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
			},
			Timeout: time.Duration(time.Second * 30),
		},
	}
}

// DirectQuery handles query requests to Zabbix
func (ds *ZabbixAPIClient) DirectQuery(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	result, queryExistInCache := ds.queryCache.Get(HashString(tsdbReq.String()))

	if queryExistInCache {
		return BuildResponse(result)
	}

	dsInfo := tsdbReq.GetDatasource()

	queries := []requestModel{}
	for _, query := range tsdbReq.Queries {
		request := requestModel{}
		err := json.Unmarshal([]byte(query.GetModelJson()), request)

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

	response, err := ds.RawRequest(ctx, dsInfo, query.Target.Method, query.Target.Params)
	ds.queryCache.Set(HashString(tsdbReq.String()), response)
	if err != nil {
		ds.logger.Debug("ZabbixAPIQuery", "error", err)
		return nil, errors.New("ZabbixAPIQuery is not implemented yet")
	}

	return BuildResponse(response)
}

// TestConnection checks authentication and version of the Zabbix API and returns that info
func (ds *ZabbixAPIClient) TestConnection(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	dsInfo := tsdbReq.GetDatasource()

	auth, err := ds.loginWithDs(ctx, dsInfo)
	if err != nil {
		return BuildErrorResponse(fmt.Errorf("Authentication failed: %w", err)), nil
	}
	ds.authToken = auth

	result, err := ds.zabbixAPIRequest(ctx, dsInfo.GetUrl(), "apiinfo.version", zabbixParams{}, "")
	if err != nil {
		ds.logger.Debug("TestConnection", "error", err)
		return BuildErrorResponse(fmt.Errorf("Version check failed: %w", err)), nil
	}

	ds.logger.Debug("TestConnection", "result", string(result))

	var version string
	err = json.Unmarshal(result, version)
	if err != nil {
		return nil, err
	}

	testResponse := connectionTestResponse{
		ZabbixVersion: version,
	}

	return BuildResponse(testResponse)
}

// RawRequest checks authentication and makes a request to the Zabbix API
func (ds *ZabbixAPIClient) RawRequest(ctx context.Context, dsInfo *datasource.DatasourceInfo, method string, params zabbixParams) (result json.RawMessage, err error) {
	zabbixURL := dsInfo.GetUrl()

	for attempt := 0; attempt <= 3; attempt++ {
		if ds.authToken == "" {
			// Authenticate
			auth, err := ds.loginWithDs(ctx, dsInfo)
			if err != nil {
				return nil, err
			}
			ds.authToken = auth
		}

		result, err = ds.zabbixAPIRequest(ctx, zabbixURL, method, params, ds.authToken)

		if err == nil || (err != nil && !isNotAuthorized(err.Error())) {
			break
		} else {
			ds.authToken = ""
		}
	}
	return result, err
}

func (ds *ZabbixAPIClient) loginWithDs(ctx context.Context, dsInfo *datasource.DatasourceInfo) (string, error) {
	zabbixURLStr := dsInfo.GetUrl()
	zabbixURL, err := url.Parse(zabbixURLStr)
	if err != nil {
		return "", err
	}

	jsonDataStr := dsInfo.GetJsonData()
	jsonData, err := simplejson.NewJson([]byte(jsonDataStr))
	if err != nil {
		return "", err
	}

	zabbixLogin := jsonData.Get("username").MustString()
	var zabbixPassword string
	if securePassword, exists := dsInfo.GetDecryptedSecureJsonData()["password"]; exists {
		zabbixPassword = securePassword
	} else {
		zabbixPassword = jsonData.Get("password").MustString()
	}

	auth, err := ds.login(ctx, zabbixURLStr, zabbixLogin, zabbixPassword)
	if err != nil {
		ds.logger.Error("loginWithDs", "error", err)
		return "", err
	}
	ds.logger.Debug("loginWithDs", "url", zabbixURL, "user", zabbixLogin, "auth", auth)

	return auth, nil
}

func (ds *ZabbixAPIClient) login(ctx context.Context, apiURL string, username string, password string) (string, error) {
	params := zabbixParams{
		User:     username,
		Password: password,
	}
	result, err := ds.zabbixAPIRequest(ctx, apiURL, "user.login", params, "")
	if err != nil {
		return "", err
	}

	var auth string
	err = json.Unmarshal(result, auth)
	if err != nil {
		return "", err
	}

	return auth, nil
}

func (ds *ZabbixAPIClient) zabbixAPIRequest(ctx context.Context, apiURL string, method string, params zabbixParams, auth string) (json.RawMessage, error) {
	zabbixURL, err := url.Parse(apiURL)

	// TODO: inject auth token (obtain from 'user.login' first)
	apiRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  method,
		"params":  params,
	}

	if auth != "" {
		apiRequest["auth"] = auth
	}

	reqBodyJSON, err := json.Marshal(apiRequest)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	body = bytes.NewReader(reqBodyJSON)
	rc, ok := body.(io.ReadCloser)
	if !ok && body != nil {
		rc = ioutil.NopCloser(body)
	}

	req := &http.Request{
		Method: "POST",
		URL:    zabbixURL,
		Header: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: rc,
	}

	response, err := makeHTTPRequest(ctx, ds.httpClient, req)
	if err != nil {
		return nil, err
	}

	ds.logger.Debug("zabbixAPIRequest", "response", string(response))

	return handleAPIResult(response)
}

func handleAPIResult(response []byte) (json.RawMessage, error) {
	var zabbixResp *zabbixResponse
	err := json.Unmarshal(response, &zabbixResp)

	if err != nil {
		return nil, err
	}

	if zabbixResp.Error != nil {
		return nil, fmt.Errorf("Code %d: '%s' %s", zabbixResp.Error.Code, zabbixResp.Error.Message, zabbixResp.Error.Data)
	}

	return zabbixResp.Result, nil
}

func makeHTTPRequest(ctx context.Context, httpClient *http.Client, req *http.Request) ([]byte, error) {
	res, err := ctxhttp.Do(ctx, httpClient, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid status code. status: %v", res.Status)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func isNotAuthorized(message string) bool {
	return message == "Session terminated, re-login, please." ||
		message == "Not authorised." ||
		message == "Not authorized."
}

func (ds *ZabbixAPIClient) GetFilteredItems(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string, appids []string, itemtype string) (zabbix.Items, error) {
	params := zabbixParams{
		Output:      &zabbixParamOutput{Fields: []string{"itemid", "name", "key_", "value_type", "hostid", "status", "state"}},
		SortField:   "name",
		WebItems:    true,
		Filter:      map[string][]int{},
		SelectHosts: []string{"hostid", "name"},
		HostIDs:     hostids,
		AppIDs:      appids,
	}

	if itemtype == "num" {
		params.Filter["value_type"] = []int{0, 3}
	} else if itemtype == "text" {
		params.Filter["value_type"] = []int{1, 2, 4}
	}

	result, err := ds.RawRequest(ctx, dsInfo, "item.get", params)
	if err != nil {
		return nil, err
	}

	var items zabbix.Items
	err = json.Unmarshal(result, items)
	if err != nil {
		return nil, err
	}

	return items, nil
}

func (ds *ZabbixAPIClient) GetAppsByHostIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string) (zabbix.Applications, error) {
	params := zabbixParams{Output: &zabbixParamOutput{Mode: "extend"}, HostIDs: hostids}
	result, err := ds.RawRequest(ctx, dsInfo, "application.get", params)
	if err != nil {
		return nil, err
	}

	var apps zabbix.Applications
	err = json.Unmarshal(result, apps)
	if err != nil {
		return nil, err
	}

	return apps, nil

}

func (ds *ZabbixAPIClient) GetHostsByGroupIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupids []string) (zabbix.Hosts, error) {
	params := zabbixParams{Output: &zabbixParamOutput{Fields: []string{"hostid", "name", "host"}}, SortField: "name", GroupIDs: groupids}
	result, err := ds.RawRequest(ctx, dsInfo, "host.get", params)
	if err != nil {
		return nil, err
	}

	var hosts zabbix.Hosts
	err = json.Unmarshal(result, hosts)
	if err != nil {
		return nil, err
	}

	return hosts, nil
}

func (ds *ZabbixAPIClient) GetAllGroups(ctx context.Context, dsInfo *datasource.DatasourceInfo) (zabbix.Groups, error) {
	params := zabbixParams{Output: &zabbixParamOutput{Fields: []string{"groupid", "name"}}, SortField: "name", RealHosts: true}
	result, err := ds.RawRequest(ctx, dsInfo, "hostgroup.get", params)
	if err != nil {
		return nil, err
	}

	var groups zabbix.Groups
	err = json.Unmarshal(result, groups)
	if err != nil {
		return nil, err
	}

	return groups, nil
}

func (ds *ZabbixAPIClient) GetHistory(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items zabbix.Items) (zabbix.History, error) {
	totalHistory := zabbix.History{}

	timeRange := tsdbReq.GetTimeRange()
	groupedItems := map[int]zabbix.Items{}

	for _, item := range items {
		groupedItems[item.ValueType] = append(groupedItems[item.ValueType], item)
	}

	for valueType, items := range groupedItems {
		var itemids []string
		for _, item := range items {
			itemids = append(itemids, item.ID)
		}
		params := zabbixParams{
			Output:    &zabbixParamOutput{Mode: "extend"},
			SortField: "clock",
			SortOrder: "ASC",
			ItemIDs:   itemids,
			TimeFrom:  timeRange.GetFromEpochMs(),
			TimeTill:  timeRange.GetToEpochMs(),
			History:   valueType,
		}

		var history zabbix.History
		result, err := ds.RawRequest(ctx, tsdbReq.GetDatasource(), "history.get", params)
		if err != nil {
			return nil, err
		}

		json.Unmarshal(result, history)
		if err != nil {
			return nil, err
		}

		totalHistory = append(totalHistory, history...)
	}
	return totalHistory, nil
}

func (ds *ZabbixAPIClient) GetTrend(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items zabbix.Items) (zabbix.Trend, error) {
	timeRange := tsdbReq.GetTimeRange()

	var itemids []string
	for _, item := range items {
		itemids = append(itemids, item.ID)
	}
	params := zabbixParams{
		Output:    &zabbixParamOutput{Mode: "extend"},
		SortField: "clock",
		SortOrder: "ASC",
		ItemIDs:   itemids,
		TimeFrom:  timeRange.GetFromEpochMs(),
		TimeTill:  timeRange.GetToEpochMs(),
	}

	var trend zabbix.Trend
	result, err := ds.RawRequest(ctx, tsdbReq.GetDatasource(), "trend.get", params)
	if err != nil {
		return nil, err
	}

	json.Unmarshal(result, trend)
	if err != nil {
		return nil, err
	}

	return trend, nil
}
