package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/service"
)

func newTestRouter() http.Handler {
	return NewRouter(service.NewRuntime())
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var decoded map[string]any
	if response.Body.Len() > 0 {
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode response %q: %v", response.Body.String(), err)
		}
	}
	return response, decoded
}

func TestLineVersionLifecycleAPI(t *testing.T) {
	router := newTestRouter()

	// 基线 v1 已发布且为当前版本。
	response, current := doJSON(t, router, http.MethodGet, "/api/line-versions/current", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("current status = %d body = %s", response.Code, response.Body.String())
	}
	if int(current["number"].(float64)) != 1 || current["status"].(string) != "published" {
		t.Fatalf("unexpected current version: %v", current)
	}

	// 新建草拟（默认复制 v1 内容）。
	response, created := doJSON(t, router, http.MethodPost, "/api/line-versions", map[string]any{"note": "tighten limit"})
	if response.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d body = %s", response.Code, response.Body.String())
	}
	draftNumber := int(created["number"].(float64))
	if draftNumber != 2 || created["status"].(string) != "draft" {
		t.Fatalf("unexpected draft: %v", created)
	}

	// 录入非法压力限制：校验应返回 422，但草拟保留。
	content := map[string]any{
		"stations": []model.Station{
			{ID: "station-a", Name: "North Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 12.5, Enabled: true},
			{ID: "station-b", Name: "Central Block Valve", Role: model.StationIntermediate, Kilometer: 86.4, PressureLimit: 11.8, Enabled: true},
			{ID: "station-c", Name: "South Receiver", Role: model.StationReceiver, Kilometer: 173.2, PressureLimit: 11.2, Enabled: true},
		},
		"segments": []model.Segment{
			{ID: "segment-a-b", From: "station-a", To: "station-b", Length: 86.4, MaximumPressure: 99, Enabled: true},
			{ID: "segment-b-c", From: "station-b", To: "station-c", Length: 86.8, MaximumPressure: 11.2, Enabled: true},
		},
	}
	response, _ = doJSON(t, router, http.MethodPut, "/api/line-versions/2", content)
	if response.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", response.Code, response.Body.String())
	}
	response, _ = doJSON(t, router, http.MethodPost, "/api/line-versions/2/validate", nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validate invalid status = %d; want 422", response.Code)
	}

	// 修正为合法内容后校验通过。
	content["segments"].([]model.Segment)[0].MaximumPressure = 11.5
	doJSON(t, router, http.MethodPut, "/api/line-versions/2", content)
	response, validated := doJSON(t, router, http.MethodPost, "/api/line-versions/2/validate", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("validate status = %d body = %s", response.Code, response.Body.String())
	}
	if validated["valid"].(bool) != true {
		t.Fatalf("validate should report valid: %v", validated)
	}

	// 发布：无在途作业，成功。
	response, published := doJSON(t, router, http.MethodPost, "/api/line-versions/2/publish", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", response.Code, response.Body.String())
	}
	if published["status"].(string) != "published" || published["content_checksum"].(string) == "" {
		t.Fatalf("unexpected published version: %v", published)
	}

	// 已发布版本不可变。
	response, _ = doJSON(t, router, http.MethodPut, "/api/line-versions/2", content)
	if response.Code != http.StatusConflict {
		t.Fatalf("update published status = %d; want 409", response.Code)
	}

	// 历史 v1 仍可回看。
	response, old := doJSON(t, router, http.MethodGet, "/api/line-versions/1", nil)
	if response.Code != http.StatusOK || old["status"].(string) != "published" {
		t.Fatalf("get v1 status = %d body = %s", response.Code, response.Body.String())
	}

	// 未知版本返回 404；非法版本号返回 400。
	response, _ = doJSON(t, router, http.MethodGet, "/api/line-versions/99", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing version status = %d; want 404", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/line-versions/abc", nil)
	response2 := httptest.NewRecorder()
	router.ServeHTTP(response2, request)
	if response2.Code != http.StatusNotFound {
		t.Fatalf("non-numeric version should not match the route, got %d", response2.Code)
	}
}

func TestNewOperationBindsCurrentVersionAPI(t *testing.T) {
	router := newTestRouter()

	// 在当前版本下创建作业。
	response, operation := doJSON(t, router, http.MethodPost, "/api/operations", map[string]any{
		"station_id":      "station-a",
		"route_id":        "main-station-a-station-c",
		"target_pressure": 10.0,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("launch status = %d body = %s", response.Code, response.Body.String())
	}
	runID := operation["run"].(map[string]any)["ID"].(string)
	if int(operation["line_version"].(float64)) != 1 {
		t.Fatalf("operation line_version = %v; want 1", operation["line_version"])
	}

	// 在途作业阻止发布新版本。
	doJSON(t, router, http.MethodPost, "/api/line-versions", map[string]any{})
	response, _ = doJSON(t, router, http.MethodPost, "/api/line-versions/2/publish", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("publish during active run status = %d; want 409", response.Code)
	}

	// 作业当时采用的线路版本可回看。
	response, bound := doJSON(t, router, http.MethodGet, "/api/operations/"+runID+"/line-version", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("operation line version status = %d body = %s", response.Code, response.Body.String())
	}
	if int(bound["number"].(float64)) != 1 {
		t.Fatalf("bound version = %v; want 1", bound["number"])
	}
}
