package apiserver

import (
	"reflect"
	"testing"

	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetEventApply(t *testing.T) {
	target := target{
		Address:   "1.1.1.1",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "created",
	}
	event, err := getEvent(target, 0)
	if event != core.EventApply {
		t.Errorf("getEvent(target) = %d, want core.EventApply", event)
	}
	if err != nil {
		t.Errorf("getEvent(target) returns err: %s", err)
	}
}

func TestGetEventDelete(t *testing.T) {
	target := target{
		Address:   "1.1.1.1",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "deleted",
	}
	event, err := getEvent(target, 0)
	if event != core.EventDelete {
		t.Errorf("getEvent(target) = %d, want core.EventDelete", event)
	}
	if err != nil {
		t.Errorf("getEvent(target) returns err: %s", err)
	}
}

func TestGetEventEmptyOperation(t *testing.T) {
	target := target{
		Address:   "1.1.1.1",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "",
	}
	event, err := getEvent(target, 0)
	if err == nil {
		t.Errorf("getEvent(target, 0) = %d, want error", event)
	}
}

func TestGetEventUpdate(t *testing.T) {
	target := target{
		Address:   "1.1.1.1",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "updated",
	}
	event, err := getEvent(target, 0)
	if event != core.EventApply {
		t.Errorf("getEvent(target) = %d, want core.EventApply", event)
	}
	if err != nil {
		t.Errorf("getEvent(target) returns err: %s", err)
	}
}

func TestGetKey(t *testing.T) {
	u := urlStruct{
		Namespace: "default",
		Name:      "http-discovery",
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      "http-discovery",
	}
	result := getKey(u)
	if result != expected {
		t.Errorf("getKey(%v) = %v; want %v", u, result, expected)
	}
}

func TestConvertTargetLabelsToMapEmpty(t *testing.T) {
	target := target{}
	result := convertTargetLabelsToMap(target)
	if len(result) != 0 {
		t.Errorf("convertTargetLabelsToMap(target) = %v; want empty map", result)
	}
}

func TestConvertTargetLabelsToMap(t *testing.T) {
	key := "Tag"
	value := "TT1, TT2"
	label := Label{
		Key:   &key,
		Value: &value,
	}
	target := target{
		Labels: &[]Label{label},
	}
	expected := map[string]string{
		"Tag": "TT1, TT2",
	}
	result := convertTargetLabelsToMap(target)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertTargetLabelsToMap(target) = %v; want %v", result, expected)
	}
}

func TestConvertTargetLabelsToMapEmptyKey(t *testing.T) {
	key := ""
	value := "TT1, TT2"
	label := Label{
		Key:   &key,
		Value: &value,
	}
	target := target{
		Labels: &[]Label{label},
	}
	result := convertTargetLabelsToMap(target)
	if len(result) != 0 {
		t.Errorf("convertTargetLabelsToMap(target) = %v; want empty map", result)
	}
}

func TestConvertTargetLabelsToMapTwoEntries(t *testing.T) {
	key := "Tag"
	key2 := "Tag1"
	value := "TT1, TT2"
	value2 := "TT1"
	label := Label{
		Key:   &key,
		Value: &value,
	}
	label2 := Label{
		Key:   &key2,
		Value: &value2,
	}
	target := target{
		Labels: &[]Label{label, label2},
	}
	expected := map[string]string{
		"Tag":  "TT1, TT2",
		"Tag1": "TT1",
	}
	result := convertTargetLabelsToMap(target)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertTargetLabelsToMap(target) = %v; want %v", result, expected)
	}
}

func TestCreateDiscoveryEvent(t *testing.T) {
	targets := []target{{
		Address:   "1.1.1.1",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "updated"}}

	expected := []core.DiscoveryEvent{
		{
			Target: core.DiscoveredTarget{
				Name:    "routername",
				Address: "1.1.1.1",
				Labels:  map[string]string{},
			},
			Event: core.EventApply,
		},
	}
	result, _ := createDiscoveryEvent(targets)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("createDiscoveryEvent(targets) = %v; want %v", result, expected)
	}
}

func TestCreateDiscoveryEventEmptyName(t *testing.T) {
	targets := []target{{
		Address:   "1.1.1.1",
		Name:      "",
		Labels:    &[]Label{},
		Operation: "updated"}}

	result, err := createDiscoveryEvent(targets)
	if err == nil {
		t.Errorf("createDiscoveryEvent(targets) returns %v, want missing name error", result)
	}
}

func TestCreateDiscoveryEventEmptyAddress(t *testing.T) {
	targets := []target{{
		Address:   "",
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "updated"}}

	result, err := createDiscoveryEvent(targets)
	if err == nil {
		t.Errorf("createDiscoveryEvent(targets) returns %v, want missing address error", result)
	}
}

func TestCreateDiscoveryEventWrongEvent(t *testing.T) {
	targets := []target{{
		Address:   "1.1.1.1",
		Name:      "",
		Labels:    &[]Label{},
		Operation: "wrongOperation"}}

	result, err := createDiscoveryEvent(targets)
	if err == nil {
		t.Errorf("createDiscoveryEvent(targets) returns %v, want wrong Operation error", result)
	}
}

func TestParseURI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	var result urlStruct
	router.POST("/api/v1/:namespace/target-source/:name/createTargets", func(ctx *gin.Context) {
		result = parseURI(ctx)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/default/target-source/http-discovery/createTargets", nil)
	router.ServeHTTP(recorder, req)

	expected := urlStruct{
		Namespace: "default",
		Name:      "http-discovery",
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("parseURI(ctx) = %v; want %v", result, expected)
	}
	if recorder.Code != http.StatusOK {
		t.Errorf("parseURI(ctx) status code = %d; want %d", recorder.Code, http.StatusOK)
	}
}

func TestParseURIMissingName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	var result urlStruct
	router.POST("/api/v1/:namespace/target-source/:name/createTargets", func(ctx *gin.Context) {
		result = parseURI(ctx)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/default/target-source//createTargets", nil)
	router.ServeHTTP(recorder, req)

	if !reflect.DeepEqual(result, urlStruct{}) {
		t.Errorf("parseURI(ctx) = %v; want empty urlStruct", result)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("parseURI(ctx) status code = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
}
