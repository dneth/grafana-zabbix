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

	if !queryExistInCache {
		dsInfo := tsdbReq.GetDatasource()

		jsonQueries := make([]*queryRequest, 0)
		for _, query := range tsdbReq.Queries {
			var request *queryRequest
			err := json.Unmarshal([]byte(query.GetModelJson()), request)

			if err != nil {
				return nil, err
			}

			ds.logger.Debug("ZabbixAPIQuery", "method", request.Method, "params", request.Params)

			jsonQueries = append(jsonQueries, request)
		}

		if len(jsonQueries) == 0 {
			return nil, errors.New("At least one query should be provided")
		}

		jsonQuery := jsonQueries[0]

		response, err := ds.RawRequest(ctx, dsInfo, jsonQuery.Method, jsonQuery.Params)
		ds.queryCache.Set(HashString(tsdbReq.String()), response)
		result = response
		if err != nil {
			ds.logger.Debug("ZabbixAPIQuery", "error", err)
			return nil, errors.New("ZabbixAPIQuery is not implemented yet")
		}
	}

	resultByte, _ := result.(*simplejson.Json).MarshalJSON()
	ds.logger.Debug("ZabbixAPIQuery", "result", string(resultByte))

	return BuildResponse(result)
}

// TestConnection checks authentication and version of the Zabbix API and returns that info
func (ds *ZabbixAPIClient) TestConnection(ctx context.Context, tsdbReq *datasource.DatasourceRequest) (*datasource.DatasourceResponse, error) {
	dsInfo := tsdbReq.GetDatasource()

	auth, err := ds.loginWithDs(ctx, dsInfo)
	if err != nil {
		return BuildErrorResponse(fmt.Errorf("Authentication failed: %w", err)), nil
	}
	ds.authToken = auth

	response, err := ds.zabbixAPIRequest(ctx, dsInfo.GetUrl(), "apiinfo.version", zabbixParams{}, "")
	if err != nil {
		ds.logger.Debug("TestConnection", "error", err)
		return BuildErrorResponse(fmt.Errorf("Version check failed: %w", err)), nil
	}

	resultByte, _ := response.MarshalJSON()
	ds.logger.Debug("TestConnection", "result", string(resultByte))

	testResponse := connectionTestResponse{
		ZabbixVersion: response.MustString(),
	}

	return BuildResponse(testResponse)
}

// RawRequest checks authentication and makes a request to the Zabbix API
func (ds *ZabbixAPIClient) RawRequest(ctx context.Context, dsInfo *datasource.DatasourceInfo, method string, params zabbixParams) (result *simplejson.Json, err error) {
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
	auth, err := ds.zabbixAPIRequest(ctx, apiURL, "user.login", params, "")
	if err != nil {
		return "", err
	}

	return auth.MustString(), nil
}

func (ds *ZabbixAPIClient) zabbixAPIRequest(ctx context.Context, apiURL string, method string, params zabbixParams, auth string) (*simplejson.Json, error) {
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

func handleAPIResult(response []byte) (*simplejson.Json, error) {
	jsonResp, err := simplejson.NewJson([]byte(response))
	if err != nil {
		return nil, err
	}
	if errJSON, isError := jsonResp.CheckGet("error"); isError {
		errMessage := fmt.Sprintf("%s %s", errJSON.Get("message").MustString(), errJSON.Get("data").MustString())
		return nil, errors.New(errMessage)
	}
	jsonResult := jsonResp.Get("result")
	return jsonResult, nil
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

func (ds *ZabbixAPIClient) GetFilteredItems(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string, appids []string, itemtype string) (*simplejson.Json, error) {
	params := zabbixParams{
		Output:      []string{"name", "_key", "value_type", "hostid", "status", "state"},
		SortField:   "name",
		WebItems:    true,
		Filter:      map[string][]string{},
		SelectHosts: []string{"hostid", "name"},
		HostIDs:     hostids,
		AppIDs:      appids,
	}

	if itemtype == "num" {
		params.Filter["value_type"] = []string{"0", "3"}
	} else if itemtype == "text" {
		params.Filter["value_type"] = []string{"1", "2", "4"}
	}

	return ds.RawRequest(ctx, dsInfo, "item.get", params)
}

func (ds *ZabbixAPIClient) GetAppsByHostIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, hostids []string) (*simplejson.Json, error) {
	params := zabbixParams{Output: []string{"extend"}, HostIDs: hostids}
	return ds.RawRequest(ctx, dsInfo, "application.get", params)
}

func (ds *ZabbixAPIClient) GetHostsByGroupIDs(ctx context.Context, dsInfo *datasource.DatasourceInfo, groupids []string) (*simplejson.Json, error) {
	params := zabbixParams{Output: []string{"name", "host"}, SortField: "name", GroupIDs: groupids}
	return ds.RawRequest(ctx, dsInfo, "host.get", params)
}

func (ds *ZabbixAPIClient) GetAllGroups(ctx context.Context, dsInfo *datasource.DatasourceInfo) (*simplejson.Json, error) {
	params := zabbixParams{Output: []string{"name"}, SortField: "name", RealHosts: true}
	return ds.RawRequest(ctx, dsInfo, "hostgroup.get", params)
}

func (ds *ZabbixAPIClient) GetHistoryOrTrend(ctx context.Context, tsdbReq *datasource.DatasourceRequest, items []*simplejson.Json, useTrend bool) ([]*simplejson.Json, error) {
	var result []*simplejson.Json
	var response *simplejson.Json
	timeRange := tsdbReq.GetTimeRange()
	groupedItems := map[int][]*simplejson.Json{}

	for _, j := range items {
		groupedItems[j.Get("value_type").MustInt()] = append(groupedItems[j.Get("value_type").MustInt()], j)
	}

	for k, l := range groupedItems {
		var itemids []string
		for _, m := range l {
			itemids = append(itemids, m.Get("itemid").MustString())
		}
		params := zabbixParams{Output: []string{"extend"}, SortField: "clock", SortOrder: "ASC", ItemIDs: itemids, TimeFrom: timeRange.GetFromRaw(), TimeTill: timeRange.GetToRaw()}

		var err error

		if useTrend {
			response, err = ds.RawRequest(ctx, tsdbReq.GetDatasource(), "trend.get", params)
		} else {
			params.History = k
			response, err = ds.RawRequest(ctx, tsdbReq.GetDatasource(), "history.get", params)
		}

		if err != nil {
			return nil, err
		}
		result = append(result, response)
	}
	return result, nil
}
