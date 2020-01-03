package main

import (
	"testing"

	simplejson "github.com/bitly/go-simplejson"
	"github.com/grafana/grafana_plugin_model/go/datasource"
	hclog "github.com/hashicorp/go-hclog"
	cache "github.com/patrickmn/go-cache"
	"gotest.tools/assert"
	"gotest.tools/assert/cmp"
)

func TestZabbixBackend_getCachedDatasource(t *testing.T) {
	basicDatasourceInfo := &datasource.DatasourceInfo{
		Id:   1,
		Name: "TestDatasource",
	}
	basicDatasourceInfoHash := HashDatasourceInfo(basicDatasourceInfo)

	altDatasourceInfo := &datasource.DatasourceInfo{
		Id:   2,
		Name: "AnotherDatasource",
	}
	altDatasourceInfoHash := HashDatasourceInfo(altDatasourceInfo)

	modifiedDatasource := NewZabbixDatasourceWithHash(hclog.NewNullLogger(), "UNIQUE_HASH")

	tests := []struct {
		name    string
		cache   *cache.Cache
		request *datasource.DatasourceRequest
		want    *ZabbixDatasource
	}{
		{
			name: "Uncached Datasource (nothing in cache)",
			request: &datasource.DatasourceRequest{
				Datasource: basicDatasourceInfo,
			},
			want: NewZabbixDatasourceWithHash(hclog.NewNullLogger(), HashDatasourceInfo(basicDatasourceInfo)),
		},
		{
			name: "Uncached Datasource (cache miss)",
			cache: cache.NewFrom(cache.NoExpiration, cache.NoExpiration, map[string]cache.Item{
				basicDatasourceInfoHash: cache.Item{Object: modifiedDatasource},
			}),
			request: &datasource.DatasourceRequest{
				Datasource: altDatasourceInfo,
			},
			want: NewZabbixDatasourceWithHash(hclog.NewNullLogger(), HashDatasourceInfo(altDatasourceInfo)),
		},
		{
			name: "Cached Datasource",
			cache: cache.NewFrom(cache.NoExpiration, cache.NoExpiration, map[string]cache.Item{
				altDatasourceInfoHash:   cache.Item{Object: NewZabbixDatasource(hclog.NewNullLogger())},
				basicDatasourceInfoHash: cache.Item{Object: modifiedDatasource},
			}),
			request: &datasource.DatasourceRequest{
				Datasource: basicDatasourceInfo,
			},
			want: modifiedDatasource,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cache == nil {
				tt.cache = cache.New(cache.NoExpiration, cache.NoExpiration)
			}
			b := &ZabbixBackend{
				logger: hclog.New(&hclog.LoggerOptions{
					Name:  "TestZabbixBackend_getCachedDatasource",
					Level: hclog.LevelFromString("DEBUG"),
				}),
				datasourceCache: &Cache{cache: tt.cache},
			}
			got := b.getCachedDatasource(tt.request)

			// Only checking the hash, being the easiest value to, and guarantee equality for
			assert.Equal(t, tt.want.hash, got.hash)

			// Ensure the datasource is in the cache
			cacheds, ok := tt.cache.Get(HashDatasourceInfo(tt.request.GetDatasource()))
			assert.Equal(t, true, ok)
			assert.Equal(t, got, cacheds)
		})
	}
}

func TestBuildResponse(t *testing.T) {
	jsonData := simplejson.New()
	jsonData.Set("testing", []int{5, 12, 75})

	tests := []struct {
		name         string
		responseData interface{}
		want         *datasource.DatasourceResponse
		wantErr      string
	}{
		{
			name:         "simplejson Response",
			responseData: jsonData,
			want: &datasource.DatasourceResponse{
				Results: []*datasource.QueryResult{
					&datasource.QueryResult{
						RefId:    "zabbixAPI",
						MetaJson: `{"testing":[5,12,75]}`,
					},
				},
			},
		},
		{
			name: "Connetion Status Response",
			responseData: connectionTestResponse{
				ZabbixVersion: "2.4",
				DbConnectorStatus: &dbConnectionStatus{
					DsType: "mysql",
					DsName: "MyDatabase",
				},
			},
			want: &datasource.DatasourceResponse{
				Results: []*datasource.QueryResult{
					&datasource.QueryResult{
						RefId:    "zabbixAPI",
						MetaJson: `{"zabbixVersion":"2.4","dbConnectorStatus":{"dsType":"mysql","dsName":"MyDatabase"}}`,
					},
				},
			},
		},
		{
			name:         "Unmarshalable",
			responseData: 2i,
			wantErr:      "json: unsupported type: complex128",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildResponse(tt.responseData)
			if tt.wantErr != "" {
				assert.Error(t, err, tt.wantErr)
				assert.Assert(t, cmp.Nil(got))
				return
			}
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}
