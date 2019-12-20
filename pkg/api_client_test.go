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
	"golang.org/x/net/context"
)

type mockZabbixAPIClient struct {
	ZabbixAPIClient
}

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

var mockDataSource = mockZabbixAPIClient{
	ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: NewTestClient(func(req *http.Request) *http.Response {

			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString(`{"result":"sampleResult"}`)),
				Header:     make(http.Header),
			}
		}),
		authToken: "sampleAuthToken",
		logger:    hclog.Default(),
	},
}

var mockDataSourceError = mockZabbixAPIClient{
	ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: NewTestClient(func(req *http.Request) *http.Response {
			return &http.Response{
				StatusCode: 500,
				Body:       ioutil.NopCloser(bytes.NewBufferString(`{"result":"sampleResult"}`)),
				Header:     make(http.Header),
			}
		}),
		authToken: "sampleAuthToken",
		logger:    hclog.Default(),
	},
}

// func TestDirectQuery(t *testing.T) {
// 	tests := []struct {
// 		name      string
// 		request string
// 		mockResponse      MockResponse
// 		wantErr string
// 	}{
// 		{
// 			name: "History time",
// 			timeRange: &datasource.TimeRange{
// 				FromEpochMs: time.Now().Add(-time.Hour*48).Unix() * 1000,
// 				ToEpochMs:   time.Now().Add(-time.Hour*12).Unix() * 1000,
// 			},
// 			want: false,
// 		},
// 		{
// 			name: "Trend time (past 7 days)",
// 			timeRange: &datasource.TimeRange{
// 				FromEpochMs: time.Now().Add(-time.Hour*24*14).Unix() * 1000,
// 				ToEpochMs:   time.Now().Add(-time.Hour*24*13).Unix() * 1000,
// 			},
// 			want: true,
// 		},
// 		{
// 			name: "Trend time (longer than 4 days)",
// 			timeRange: &datasource.TimeRange{
// 				FromEpochMs: time.Now().Add(-time.Hour*24*8).Unix() * 1000,
// 				ToEpochMs:   time.Now().Add(-time.Hour*24*1).Unix() * 1000,
// 			},
// 			want: true,
// 		},
// 	}
// 	for _, tt := range tests {
// 		mockClient := NewMockZabbixAPIClient(t, tt.request, tt.mockResponse)
// 		resp, err := mockClient.DirectQuery(context.Background(), mockDataSourceRequest(tt.request))
// 		if tt.wantErr != ""{
// 			assert.Error(t, err)
// 			assert.Nil(t, resp)
// 			assert.EqualError(t, err, tt.wantErr)
// 			return
// 		}

// 		assert.Equal(t, "\"sampleResult\"", resp.GetResults()[0].GetMetaJson())
// 		assert.Equal(t, "zabbixAPI", resp.GetResults()[0].GetRefId())
// 		assert.Nil(t, err)
// 	}
// }

// func TestDirectQueryEmptyQuery(t *testing.T) {
// 	resp, err := mockDataSource.DirectQuery(context.Background(), mockDataSourceRequest(``))

// 	assert.Nil(t, resp)
// 	assert.NotNil(t, err)
// }

// func TestDirectQueryNoQueries(t *testing.T) {
// 	basicDatasourceRequest := &datasource.DatasourceRequest{
// 		Datasource: &datasource.DatasourceInfo{
// 			Id:   1,
// 			Name: "TestDatasource",
// 		},
// 	}
// 	resp, err := mockDataSource.DirectQuery(context.Background(), basicDatasourceRequest)

// 	assert.Nil(t, resp)
// 	assert.Equal(t, "At least one query should be provided", err.Error())
// }

// func TestDirectQueryError(t *testing.T) {
// 	resp, err := mockDataSourceError.DirectQuery(context.Background(), mockDataSourceRequest(`{"target":{"method":"Method","params":{"param1" : "Param1"}}}`))

// 	assert.Nil(t, resp)
// 	assert.Error(t, err)
// 	assert.EqualError(t, err, "Error in direct query: error")
// }

func TestLogin(t *testing.T) {
	resp, err := mockDataSource.login(context.Background(), "apiURL", "username", "password")

	assert.Equal(t, "sampleResult", resp)
	assert.Nil(t, err)
}

func TestLoginError(t *testing.T) {
	resp, err := mockDataSourceError.login(context.Background(), "apiURL", "username", "password")

	assert.Equal(t, "", resp)
	assert.NotNil(t, err)
}

func TestLoginWithDs(t *testing.T) {
	resp, err := mockDataSource.loginWithDs(context.Background(), basicDatasourceInfo)

	assert.Equal(t, "sampleResult", resp)
	assert.Nil(t, err)
}

func TestLoginWithDsError(t *testing.T) {
	resp, err := mockDataSourceError.loginWithDs(context.Background(), basicDatasourceInfo)

	assert.Equal(t, "", resp)
	assert.NotNil(t, err)
}

func TestZabbixRequest(t *testing.T) {
	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
	assert.Equal(t, `"sampleResult"`, string(resp))
	assert.Nil(t, err)
}

func TestZabbixRequestWithNoAuthToken(t *testing.T) {
	var mockDataSource = mockZabbixAPIClient{
		ZabbixAPIClient{
			queryCache: NewCache(10*time.Minute, 10*time.Minute),
			httpClient: NewTestClient(func(req *http.Request) *http.Response {
				return &http.Response{
					StatusCode: 200,
					Body:       ioutil.NopCloser(bytes.NewBufferString(`{"result":"auth"}`)),
					Header:     make(http.Header),
				}
			}),
			logger: hclog.Default(),
		},
	}

	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
	assert.Equal(t, `"auth"`, string(resp))
	assert.Nil(t, err)
}

func TestZabbixRequestError(t *testing.T) {
	resp, err := mockDataSourceError.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
	assert.Nil(t, resp)
	assert.NotNil(t, err)
}

func TestZabbixAPIRequest(t *testing.T) {
	resp, err := mockDataSource.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

	assert.Equal(t, `"sampleResult"`, string(resp))
	assert.Nil(t, err)
}

func TestZabbixAPIRequestError(t *testing.T) {
	resp, err := mockDataSourceError.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

	assert.Nil(t, resp)
	assert.NotNil(t, err)
}

// func TestTestConnection(t *testing.T) {
// 	resp, err := mockDataSource.TestConnection(context.Background(), mockDataSourceRequest(``))

// 	assert.Equal(t, "{\"zabbixVersion\":\"sampleResult\",\"dbConnectorStatus\":null}", resp.Results[0].GetMetaJson())
// 	assert.Nil(t, err)
// }

// func TestTestConnectionError(t *testing.T) {
// 	resp, err := mockDataSourceError.TestConnection(context.Background(), mockDataSourceRequest(``))

// 	assert.Equal(t, "", resp.Results[0].GetMetaJson())
// 	assert.NotNil(t, resp.Results[0].GetError())
// 	assert.Nil(t, err)
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
