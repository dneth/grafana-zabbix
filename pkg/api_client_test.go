package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/grafana_plugin_model/go/datasource"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
)

type RoundTripFunc func(req *http.Request) *http.Response

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

//NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func NewTestClient(fn RoundTripFunc) *http.Client {
	return &http.Client{
		Transport: RoundTripFunc(fn),
	}
}

type MockResponse struct {
	Status int
	Body   string
}

func NewMockZabbixAPIClient(t *testing.T, expectedRequest string, mockResponse MockResponse) ZabbixAPIInterface {
	return &ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: NewTestClient(func(req *http.Request) *http.Response {
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("Could not read request sent to mock API client: %+v", req)
				return nil
			}
			assert.JSONEq(t, expectedRequest, string(body))

			return &http.Response{
				StatusCode: mockResponse.Status,
				Body:       ioutil.NopCloser(bytes.NewBufferString(mockResponse.Body)),
				Header:     make(http.Header),
			}
		}),
		authToken: "sampleAuthToken",
		logger:    hclog.Default(),
	}
}

// func TestDirectQueryEmptyQuery(t *testing.T) {
// 	resp, err := mockDataSource.DirectQuery(context.Background(), mockDataSourceRequest(``))
// 	}
// }

// func TestLogin(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp, err := zabbixDatasource.login(context.Background(), "apiURL", "username", "password")

// 	assert.Equal(t, "sampleResult", resp)
// 	assert.Nil(t, err)
// }

// func TestLoginError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 500)
// 	resp, err := zabbixDatasource.login(context.Background(), "apiURL", "username", "password")

// 	assert.Equal(t, "", resp)
// 	assert.NotNil(t, err)
// }

// func TestLoginWithDs(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp, err := zabbixDatasource.loginWithDs(context.Background(), basicDatasourceInfo)

// 	assert.Equal(t, "sampleResult", resp)
// 	assert.Nil(t, err)
// }

// func TestLoginWithDsError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 500)
// 	resp, err := zabbixDatasource.loginWithDs(context.Background(), basicDatasourceInfo)

// 	assert.Equal(t, "", resp)
// 	assert.NotNil(t, err)
// }

// func TestZabbixRequest(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp, err := zabbixDatasource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
// 	assert.Equal(t, "sampleResult", resp.MustString())
// 	assert.Nil(t, err)
// }

// func TestZabbixRequestWithNoAuthToken(t *testing.T) {
// 	var mockDataSource = ZabbixAPIClient{
// 		queryCache: NewCache(10*time.Minute, 10*time.Minute),
// 		httpClient: NewTestClient(func(req *http.Request) *http.Response {
// 			return &http.Response{
// 				StatusCode: 200,
// 				Body:       ioutil.NopCloser(bytes.NewBufferString(`{"result":"auth"}`)),
// 				Header:     make(http.Header),
// 			}
// 		}),
// 		logger: hclog.Default(),
// 	}

// 	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
// 	assert.Equal(t, `"auth"`, string(resp))
// 	assert.Nil(t, err)
// }

// func TestZabbixRequestError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 500)
// 	resp, err := zabbixDatasource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestZabbixAPIRequest(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp, err := zabbixDatasource.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

// 	assert.Equal(t, `"sampleResult"`, string(resp))
// 	assert.Nil(t, err)
// }

// func TestZabbixAPIRequestError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 500)
// 	resp, err := zabbixDatasource.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

func TestIsNotAuthorized(t *testing.T) {
	testPositive := isNotAuthorized("Not authorised.")
	assert.True(t, testPositive)

	testNegative := isNotAuthorized("testNegative")
	assert.False(t, testNegative)
}

func TestHandleAPIResult(t *testing.T) {
	expectedResponse, err := handleAPIResult([]byte(`{"result":"sampleResult"}`))

	assert.Equal(t, `"sampleResult"`, string(expectedResponse))
	assert.Nil(t, err)
}

func TestHandleAPIResultFormatError(t *testing.T) {
	expectedResponse, err := handleAPIResult([]byte(`{"result"::"sampleResult"}`))

	assert.NotNil(t, err)
	assert.Nil(t, expectedResponse)
}

func TestHandleAPIResultError(t *testing.T) {
	expectedResponse, err := handleAPIResult([]byte(`{"result":"sampleResult", "error":{"message":"Message", "data":"Data"}}`))

	assert.Error(t, err)
	assert.EqualError(t, err, `Code 0: 'Message' Data`)
	assert.Nil(t, expectedResponse)
}

// func TestZabbixAPIClient_getHistotyOrTrend(t *testing.T) {
// 	type args struct {
// 		items    zabbix.Items
// 		useTrend bool
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		want    zabbix.History
// 		wantErr bool
// 	}{
// 		{
// 			name: "Experiment",
// 			args: args{
// 				items: zabbix.Items{
// 					zabbix.Item{
// 						ID:        "test",
// 						Key:       "test.key",
// 						Name:      "MyTest",
// 						ValueType: 2,
// 						HostID:    "hostid",
// 						Hosts: []zabbix.ItemHost{
// 							zabbix.ItemHost{
// 								ID:   "hostid",
// 								Name: "MyHost",
// 							},
// 						},
// 						Status: "0",
// 						State:  "0",
// 					},
// 				},
// 				useTrend: false,
// 			},
// 			want: zabbix.History{
// 				zabbix.HistoryPoint{},
// 			},
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			got, err := mockDataSource.getHistotyOrTrend(context.Background(), mockDataSourceRequest("{}"), tt.args.items, tt.args.useTrend)
// 			if tt.wantErr {
// 				assert.Error(t, err)
// 				return
// 			}

// 			assert.NoError(t, err)
// 			assert.Equal(t, tt.want, got)
// 		})
// 	}
// }
// func TestGetAllGroups(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "name": "name1"},{"groupid": "46489127", "name":"name2"}]}`, 200)
// 	resp, err := zabbixDatasource.getAllGroups(context.Background(), basicDatasourceInfo)

// 	assert.Equal(t, "46489126", resp.MustArray()[0].(map[string]interface{})["groupid"])
// 	assert.Equal(t, "46489127", resp.MustArray()[1].(map[string]interface{})["groupid"])
// 	assert.Nil(t, err)
// }

// func TestGetAllHosts(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"hostid": "46489126", "name": "hostname1"},{"hostid": "46489127", "name":"hostname2"}]}`, 200)
// 	resp, err := zabbixDatasource.getAllHosts(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

// 	assert.Equal(t, "46489126", resp.MustArray()[0].(map[string]interface{})["hostid"])
// 	assert.Equal(t, "46489127", resp.MustArray()[1].(map[string]interface{})["hostid"])
// 	assert.Nil(t, err)
// }

// func TestGetAllApps(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"applicationid": "46489126", "name": "hostname1"},{"applicationid": "46489127", "name":"hostname2"}]}`, 200)
// 	resp, err := zabbixDatasource.getAllApps(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

// 	assert.Equal(t, "46489126", resp.MustArray()[0].(map[string]interface{})["applicationid"])
// 	assert.Equal(t, "46489127", resp.MustArray()[1].(map[string]interface{})["applicationid"])
// 	assert.Nil(t, err)
// }

// func TestGetAllItems(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"itemid": "46489126", "name": "hostname1"},{"itemid": "46489127", "name":"hostname2"}]}`, 200)
// 	resp, err := zabbixDatasource.getAllItems(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"}, []string{"7947934", "9182763"}, "num")

// 	assert.Equal(t, "46489126", resp.MustArray()[0].(map[string]interface{})["itemid"])
// 	assert.Equal(t, "46489127", resp.MustArray()[1].(map[string]interface{})["itemid"])
// 	assert.Nil(t, err)
// }

// func TestGetGroups(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "name": "name1"},{"groupid": "46489127", "name":"name2"}]}`, 200)
// 	resp, err := zabbixDatasource.getGroups(context.Background(), basicDatasourceInfo, "name1")

// 	assert.Equal(t, "46489126", resp[0]["groupid"])
// 	assert.Equal(t, "name1", resp[0]["name"])
// 	assert.Nil(t, err)
// }

// func TestGetGroupsError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "name": "name1"},{"groupid": "46489127", "name":"name2"}]}`, 500)
// 	resp, err := zabbixDatasource.getGroups(context.Background(), basicDatasourceInfo, "name1")

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestGetHosts(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "name": "hostname1"},{"groupid": "46489127","hostid": "846586", "name":"hostname2"}]}`, 200)
// 	resp, err := zabbixDatasource.getHosts(context.Background(), basicDatasourceInfo, "nam", "hostna")

// 	assert.Equal(t, "7468763", resp[0]["hostid"])
// 	assert.Equal(t, "hostname1", resp[0]["name"])
// 	assert.Nil(t, err)
// }

// func TestGetHostsError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "name": "hostname1"},{"groupid": "46489127","hostid": "846586", "name":"hostname2"}]}`, 500)
// 	resp, err := zabbixDatasource.getHosts(context.Background(), basicDatasourceInfo, "nam", "hostna")

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestGetApps(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "applicationid": "7947934", "name": "appname1"},
// 	{"groupid": "46489127","hostid": "846586", "applicationid": "9182763", "name": "appname2"}]}`, 200)
// 	resp, err := zabbixDatasource.getApps(context.Background(), basicDatasourceInfo, "nam", "hostnam", "appname1")

// 	assert.Equal(t, "7947934", resp[0]["applicationid"])
// 	assert.Equal(t, "appname1", resp[0]["name"])
// 	assert.Nil(t, err)
// }

// func TestGetAppsError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "applicationid": "7947934", "name": "appname1"},
// 	{"groupid": "46489127","hostid": "846586", "applicationid": "9182763", "name": "appname2"}]}`, 500)
// 	resp, err := zabbixDatasource.getApps(context.Background(), basicDatasourceInfo, "nam", "hostnam", "appname1")

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestGetItems(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "applicationid": "7947934", "itemid": "837465", "name": "itemname1", "status": "0"},
// 	{"groupid": "46489127","hostid": "846586", "applicationid": "9182763", "itemid" : "0288374", "name": "itemname2", "status": "0"}]}`, 200)
// 	resp, err := zabbixDatasource.getItems(context.Background(), basicDatasourceInfo, "name", "name", "name", "name", "num")

// 	assert.Equal(t, "837465", resp[0].ID)
// 	assert.Equal(t, "itemname1", resp[0].Name)
// 	assert.Nil(t, err)
// }

// func TestGetItemsError(t *testing.T) {
// 	zabbixDatasource := mockZabbixDataSource(`{"result":[{"groupid": "46489126", "hostid": "7468763", "applicationid": "7947934", "itemid": "837465", "name": "itemname1", "status": "0"},
// 	{"groupid": "46489127","hostid": "846586", "applicationid": "9182763", "itemid" : "0288374", "name": "itemname2", "status": "0"}]}`, 500)
// 	resp, err := zabbixDatasource.getItems(context.Background(), basicDatasourceInfo, "name", "name", "name", "name", "num")

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestGetTrendValueType(t *testing.T) {
// 	json1, _ := simplejson.NewJson([]byte(`{"functions":[{"def":{"name":"name1"}},{"def":{"name":"name2"}}]}`))
// 	json2, _ := simplejson.NewJson([]byte(`{"functions":[{"def":{"name":"name1"}},{"def":{"name":"name2"}}]}`))
// 	jsonQueries := []*simplejson.Json{json1, json2}

// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp := zabbixDatasource.getTrendValueType(jsonQueries)

// 	assert.Equal(t, "avg", resp)
// }

// func TestGetConsolidateBy(t *testing.T) {
// 	json1, _ := simplejson.NewJson([]byte(`{"functions":[{"def":{"name":"consolidateBy", "params":["sum"]}},{"def":{"name":"name2"}}]}`))
// 	json2, _ := simplejson.NewJson([]byte(`{"functions":[{"def":{"name":"name1"}},{"def":{"name":"name2"}}]}`))
// 	jsonQueries := []*simplejson.Json{json1, json2}

// 	zabbixDatasource := mockZabbixDataSource(`{"result":"sampleResult"}`, 200)
// 	resp := zabbixDatasource.getConsolidateBy(jsonQueries)

// 	assert.Equal(t, "sum", resp)

// }

func Test_isUseTrend(t *testing.T) {
	tests := []struct {
		name      string
		timeRange *datasource.TimeRange
		want      bool
	}{
		{
			name: "History time",
			timeRange: &datasource.TimeRange{
				FromEpochMs: time.Now().Add(-time.Hour*48).Unix() * 1000,
				ToEpochMs:   time.Now().Add(-time.Hour*12).Unix() * 1000,
			},
			want: false,
		},
		{
			name: "Trend time (past 7 days)",
			timeRange: &datasource.TimeRange{
				FromEpochMs: time.Now().Add(-time.Hour*24*14).Unix() * 1000,
				ToEpochMs:   time.Now().Add(-time.Hour*24*13).Unix() * 1000,
			},
			want: true,
		},
		{
			name: "Trend time (longer than 4 days)",
			timeRange: &datasource.TimeRange{
				FromEpochMs: time.Now().Add(-time.Hour*24*8).Unix() * 1000,
				ToEpochMs:   time.Now().Add(-time.Hour*24*1).Unix() * 1000,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		got := isUseTrend(tt.timeRange)
		assert.Equal(t, tt.want, got, tt.name, tt.timeRange)
	}

}
