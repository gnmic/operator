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

func stringPtr(value string) *string {
	return &value
}

func TestGetEventApply(t *testing.T) {
	port := 22
	target := Target{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
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
	port := 22
	target := Target{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
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
	port := 22
	target := Target{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
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
	port := 22
	target := Target{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
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
	target := Target{}
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
	target := Target{
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
	target := Target{
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
	target := Target{
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
	port := 22
	targetprofile := ""
	targets := []Target{{
		Name:          "router1",
		Address:       stringPtr("1.1.1.1"),
		Port:          &port,
		Labels:        &[]Label{},
		TargetProfile: &targetprofile,
		Operation:     "updated"}}

	expected := []core.DiscoveryEvent{
		{
			Target: core.DiscoveredTarget{
				Name:          "router1",
				Address:       "1.1.1.1",
				Port:          22,
				Labels:        map[string]string{},
				TargetProfile: "",
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
	port := 22
	targets := []Target{{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
		Labels:    &[]Label{},
		Operation: "updated"}}

	result, err := createDiscoveryEvent(targets)
	if err == nil {
		t.Errorf("createDiscoveryEvent(targets) returns %v, want missing name error", result)
	}
}

func TestCreateDiscoveryEventEmptyIP(t *testing.T) {
	port := 22
	targets := []Target{{
		Address:   stringPtr(""),
		Port:      &port,
		Name:      "routername",
		Labels:    &[]Label{},
		Operation: "updated"}}

	result, err := createDiscoveryEvent(targets)
	if err == nil {
		t.Errorf("createDiscoveryEvent(targets) returns %v, want missing address error", result)
	}
}

func TestCreateDiscoveryEventWrongEvent(t *testing.T) {
	port := 22
	targets := []Target{{
		Address:   stringPtr("1.1.1.1"),
		Port:      &port,
		Name:      "",
		Labels:    &[]Label{},
		Operation: "upWROOONGdated"}}

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

func TestVerifyAddress(t *testing.T) {
	address := "10.10.10.10:57400"
	expected := "10.10.10.10:57400"
	convertedAddress, _ := validateAddress(address)
	if !reflect.DeepEqual(convertedAddress, expected) {
		t.Errorf("addDefaultPortIfEmpty(address) = %s; want %s", convertedAddress, expected)
	}
}

func TestVerifyAddressIPv6(t *testing.T) {
	address := "[2345:0425:2CA1:0000:0000:0567:5673:23b5]:57400"
	expected := "2345:0425:2CA1:0000:0000:0567:5673:23b5:57400"
	convertedAddress, _ := validateAddress(address)
	if !reflect.DeepEqual(convertedAddress, expected) {
		t.Errorf("addDefaultPortIfEmpty(address) = %s; want %s", convertedAddress, expected)
	}
}

func TestVerifyAddressNoPort(t *testing.T) {
	address := "10.10.10.10:"
	expected := "10.10.10.10:57400"
	convertedAddress, err := validateAddress(address)
	if err != nil {
		t.Errorf("addDefaultPortIfEmpty(address) threw unexpected error: %s", err)
	}
	if !reflect.DeepEqual(convertedAddress, expected) {
		t.Errorf("addDefaultPortIfEmpty(address) = %s; want %s", convertedAddress, expected)
	}
}

func TestVerifyWrongAddressFormat(t *testing.T) {
	address := "10.10.10.10"
	result, err := validateAddress(address)
	if err == nil {
		t.Errorf("TestVerifyWrongAddressFormat expected error due to wrong address format(missing port), instead got: %s", result)
	}
}
