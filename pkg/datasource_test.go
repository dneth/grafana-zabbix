package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/grafana/grafana_plugin_model/go/datasource"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var basicDatasourceInfo = &datasource.DatasourceInfo{
	Id:       1,
	Name:     "TestDatasource",
	Url:      "sameUrl",
	JsonData: `{"username":"username", "password":"password"}}`,
}

func mockDataSourceRequest(modelJSON string) *datasource.DatasourceRequest {
	return &datasource.DatasourceRequest{
		Datasource: basicDatasourceInfo,
		Queries: []*datasource.Query{
			&datasource.Query{
				ModelJson: modelJSON,
			},
		},
	}
}

type MockRawRequestClient struct {
	ZabbixAPIInterface
	t              *testing.T
	expectedMethod string
	expectedParams zabbixParams
	mockResponse   string
	mockError      error
}

func (m *MockRawRequestClient) RawRequest(ctx context.Context, dsInfo *datasource.DatasourceInfo, method string, params zabbixParams) (result json.RawMessage, err error) {
	assert.Equal(m.t, m.expectedMethod, method)
	assert.Equal(m.t, m.expectedParams, params)
	if m.mockError != nil {
		return nil, m.mockError
	}

	return []byte(m.mockResponse), nil
}

func TestZabbixDatasource_DirectQuery(t *testing.T) {
	type args struct {
		ctx     context.Context
		tsdbReq *datasource.DatasourceRequest
	}
	tests := []struct {
		name          string
		request       *datasource.DatasourceRequest
		expectedQuery queryRequest
		mockResponse  string
		mockError     error
		want          *datasource.DatasourceResponse
		wantErr       error
	}{
		{
			name:          "Basic Query",
			request:       mockDataSourceRequest(`{ "target": { "method": "test.get", "params": { "user": "test" } } }`),
			expectedQuery: queryRequest{Method: "test.get", Params: zabbixParams{User: "test"}},
			mockResponse:  `"testResponse"`,
			want: &datasource.DatasourceResponse{
				Results: []*datasource.QueryResult{
					&datasource.QueryResult{
						RefId:    "zabbixAPI",
						MetaJson: `"testResponse"`,
					},
				},
			},
		},
		{
			name:    "Empty Query",
			request: mockDataSourceRequest(``),
			wantErr: fmt.Errorf("unexpected end of JSON input"),
		},
		{
			name: "No Query",
			request: &datasource.DatasourceRequest{
				Queries: []*datasource.Query{},
			},
			wantErr: fmt.Errorf("At least one query should be provided"),
		},
		{
			name:          "Error",
			request:       mockDataSourceRequest(`{ "target": { "method": "test.get", "params": { "user": "test" } } }`),
			expectedQuery: queryRequest{Method: "test.get", Params: zabbixParams{User: "test"}},
			mockError:     fmt.Errorf("Test error"),
			wantErr:       fmt.Errorf("Error in direct query: Test error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &ZabbixDatasource{
				client: &MockRawRequestClient{
					t:              t,
					expectedMethod: tt.expectedQuery.Method,
					expectedParams: tt.expectedQuery.Params,
					mockResponse:   tt.mockResponse,
					mockError:      tt.mockError,
				},
				logger: hclog.Default(),
				hash:   "testhash",
			}

			got, err := ds.DirectQuery(context.Background(), tt.request)

			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
				return
			}

			if assert.NoError(t, err) {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestZabbixDatasource_TestConnection(t *testing.T) {
	ds := &ZabbixDatasource{
		client: &MockRawRequestClient{
			t:              t,
			expectedMethod: "apiinfo.version",
			expectedParams: zabbixParams{},
			mockResponse:   `"4.0.0"`,
		},
		logger: hclog.Default(),
		hash:   "testhash",
	}

	resp, err := ds.TestConnection(context.Background(), mockDataSourceRequest(``))

	if assert.NoError(t, err) {
		assert.Equal(t, `{"zabbixVersion":"4.0.0","dbConnectorStatus":null}`, resp.Results[0].GetMetaJson())
	}
}

func TestZabbixDatasource_TestConnectionError(t *testing.T) {
	ds := &ZabbixDatasource{
		client: &MockRawRequestClient{
			t:              t,
			expectedMethod: "apiinfo.version",
			expectedParams: zabbixParams{},
			mockError:      fmt.Errorf("Test connection error"),
		},
		logger: hclog.Default(),
		hash:   "testhash",
	}

	resp, err := ds.TestConnection(context.Background(), mockDataSourceRequest(``))

	if assert.NoError(t, err) {
		assert.Equal(t, "", resp.Results[0].GetMetaJson())
		assert.Equal(t, "Version check failed: Test connection error", resp.Results[0].GetError())
	}
}

func TestZabbixDatasource_TestConnectionBadResponse(t *testing.T) {
	ds := &ZabbixDatasource{
		client: &MockRawRequestClient{
			t:              t,
			expectedMethod: "apiinfo.version",
			expectedParams: zabbixParams{},
			mockResponse:   `invalid json`,
		},
		logger: hclog.Default(),
		hash:   "testhash",
	}

	resp, err := ds.TestConnection(context.Background(), mockDataSourceRequest(``))

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.EqualError(t, err, "Internal error while parsing response from Zabbix")
}

func Test_parseFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		want    *regexp.Regexp
		wantErr string
	}{
		{
			name:   "Non-regex filter",
			filter: "foobar",
			want:   nil,
		},
		{
			name:   "Non-regex filter (would-be invalid regex)",
			filter: "fooba(r",
			want:   nil,
		},
		{
			name:   "Regex filter",
			filter: "/^foo.+/",
			want:   regexp.MustCompile("^foo.+"),
		},
		{
			name:   "Regex filter with flags",
			filter: "/^foo.+/s",
			want:   regexp.MustCompile("(?s)^foo.+"),
		},
		{
			name:    "Invalid regex",
			filter:  "/fooba(r/",
			wantErr: "error parsing regexp: missing closing ): `fooba(r`",
		},
		{
			name:    "Unsupported flag",
			filter:  "/foo.+/z",
			wantErr: "error parsing regexp: unsupported flags `z` (expected [imsU])",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilter(tt.filter)

			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
