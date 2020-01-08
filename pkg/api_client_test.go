package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/alexanderzobnin/grafana-zabbix/pkg/zabbix"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
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

func NewMockZabbixAPIClient(t *testing.T, mockResponse MockResponse) ZabbixAPIClient {
	return ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: NewTestClient(func(req *http.Request) *http.Response {
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

func TestRawRequest(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
	assert.Equal(t, `"sampleResult"`, string(resp))
	assert.Nil(t, err)
}

func TestRawRequestWithNoAuthToken(t *testing.T) {
	var mockDataSource = ZabbixAPIClient{
		queryCache: NewCache(10*time.Minute, 10*time.Minute),
		httpClient: NewTestClient(func(req *http.Request) *http.Response {
			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString(`{"result":"auth"}`)),
				Header:     make(http.Header),
			}
		}),
		logger: hclog.Default(),
	}

	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})
	assert.Equal(t, `"auth"`, string(resp))
	assert.Nil(t, err)
}

func TestRawRequestError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.RawRequest(context.Background(), basicDatasourceInfo, "method", zabbixParams{})

	assert.Nil(t, resp)
	assert.NotNil(t, err)
}

func TestLoginWithDs(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.loginWithDs(context.Background(), basicDatasourceInfo)

	assert.Equal(t, "sampleResult", resp)
	assert.Nil(t, err)
}

func TestLoginWithDsError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.loginWithDs(context.Background(), basicDatasourceInfo)

	assert.Equal(t, "", resp)
	assert.NotNil(t, err)
}

func TestLogin(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.login(context.Background(), "apiURL", "username", "password")

	assert.Equal(t, "sampleResult", resp)
	assert.Nil(t, err)
}

func TestLoginError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.login(context.Background(), "apiURL", "username", "password")

	assert.Equal(t, "", resp)
	assert.NotNil(t, err)
}

func TestZabbixAPIRequest(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

	assert.Equal(t, `"sampleResult"`, string(resp))
	assert.Nil(t, err)
}

func TestZabbixAPIRequestError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: `{"result":"sampleResult"}`})
	resp, err := mockDataSource.zabbixAPIRequest(context.Background(), "apiURL", "item.get", zabbixParams{}, "auth")

	assert.Nil(t, resp)
	assert.NotNil(t, err)
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

func TestIsNotAuthorized(t *testing.T) {
	testPositive := isNotAuthorized("Not authorised.")
	assert.True(t, testPositive)

	testNegative := isNotAuthorized("testNegative")
	assert.False(t, testNegative)
}

func TestGetAllGroups(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"groupid": "46489126", "name": "name1"},{"groupid": "46489127", "name":"name2"}]}`})
	resp, err := mockDataSource.GetAllGroups(context.Background(), basicDatasourceInfo)

	assert.Equal(t, "46489126", resp[0].ID)
	assert.Equal(t, "46489127", resp[1].ID)
	assert.Nil(t, err)
}

func TestGetAllGroupsError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: ``})
	resp, err := mockDataSource.GetAllGroups(context.Background(), basicDatasourceInfo)

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestGetHostsByGroupIDs(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"hostid": "46489126", "name": "hostname1"},{"hostid": "46489127", "name":"hostname2"}]}`})
	resp, err := mockDataSource.GetHostsByGroupIDs(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

	assert.Equal(t, "46489126", resp[0].ID)
	assert.Equal(t, "46489127", resp[1].ID)
	assert.Nil(t, err)
}

func TestGetHostsByGroupIDsError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: ``})
	resp, err := mockDataSource.GetHostsByGroupIDs(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestGetAppsByHostIDs(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"applicationid": "46489126", "name": "hostname1"},{"applicationid": "46489127", "name":"hostname2"}]}`})
	resp, err := mockDataSource.GetAppsByHostIDs(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

	assert.Equal(t, "46489126", resp[0].ID)
	assert.Equal(t, "46489127", resp[1].ID)
	assert.Nil(t, err)
}

func TestGetAppsByHostIDsError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: ``})
	resp, err := mockDataSource.GetAppsByHostIDs(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"})

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestGetFilteredItems(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"itemid": "46489126", "name": "hostname1"},{"itemid": "46489127", "name":"hostname2"}]}`})
	resp, err := mockDataSource.GetFilteredItems(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"}, []string{"7947934", "9182763"}, "num")

	assert.Equal(t, "46489126", resp[0].ID)
	assert.Equal(t, "46489127", resp[1].ID)
	assert.Nil(t, err)
}

func TestGetFilteredItemsError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 500, Body: ``})
	resp, err := mockDataSource.GetFilteredItems(context.Background(), basicDatasourceInfo, []string{"46489127", "46489127"}, []string{"7947934", "9182763"}, "num")

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestGetHistory(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"itemid": "46489126", "name": "hostname1"},{"itemid": "46489127", "name":"hostname2"}]}`})
	resp, err := mockDataSource.GetHistory(context.Background(), mockDataSourceRequest(""), zabbix.Items{zabbix.Item{ID: "13244235"}, zabbix.Item{ID: "82643598"}})

	assert.Equal(t, "46489126", resp[0].ItemID)
	assert.Equal(t, "46489127", resp[1].ItemID)
	assert.Nil(t, err)
}

func TestGetHistoryError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: ``})
	resp, err := mockDataSource.GetHistory(context.Background(), mockDataSourceRequest(""), zabbix.Items{zabbix.Item{ID: "13244235"}, zabbix.Item{ID: "82643598"}})

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func TestGetTrend(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: `{"result":[{"itemid": "46489126", "name": "hostname1"},{"itemid": "46489127", "name":"hostname2"}]}`})
	resp, err := mockDataSource.GetTrend(context.Background(), mockDataSourceRequest(""), zabbix.Items{zabbix.Item{ID: "13244235"}, zabbix.Item{ID: "82643598"}})

	assert.Equal(t, "46489126", resp[0].ItemID)
	assert.Equal(t, "46489127", resp[1].ItemID)
	assert.Nil(t, err)
}

func TestGetTrendError(t *testing.T) {
	mockDataSource := NewMockZabbixAPIClient(t, MockResponse{Status: 200, Body: ``})
	resp, err := mockDataSource.GetTrend(context.Background(), mockDataSourceRequest(""), zabbix.Items{zabbix.Item{ID: "13244235"}, zabbix.Item{ID: "82643598"}})

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}
