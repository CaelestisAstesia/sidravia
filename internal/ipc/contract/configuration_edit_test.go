package contract

import "testing"

func TestAtomicConfigurationEditStrictPayload(t *testing.T) {
	for _, payload := range []string{`{"configurationId":"a","username":"new","password":"new-secret","allowInsecureStorage":true}`, `{"configurationId":"a","password":""}`, `{"configurationId":"a","username":"new"}`} {
		if _, err := DecodeConfigurationUpdatePayload([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	for _, payload := range []string{`{"configurationId":"a","password":null}`, `{"configurationId":"a","username":"new","allowInsecureStorage":null}`, `{"configurationId":"a","password":"x","password":"y"}`, `{"configurationId":"a","password":"x","extra":true}`, `{"configurationId":"a","allowInsecureStorage":true}`} {
		if _, err := DecodeConfigurationUpdatePayload([]byte(payload)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
	for _, payload := range []string{`{"configurationId":"a"}`, `{"configurationId":"a","allowInsecureStorage":true}`} {
		if _, err := DecodeConfigurationRemovePayload([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	for _, payload := range []string{`{"configurationId":"a","allowInsecureStorage":null}`, `{"configurationId":"a","password":"x"}`} {
		if _, err := DecodeConfigurationRemovePayload([]byte(payload)); err == nil {
			t.Fatal("invalid remove accepted")
		}
	}
}
