package apiserver

import "testing"

func TestGenereateBearerToken(t *testing.T) {
	_, err := getRandomString()
	if err != nil {
		t.Errorf("generateBearerToken returns err: %s", err)
	}
}
