package main

import (
	"testing"
)

// Typed-path extraction: full proposal body shape.
func TestExtractPaymentIDTyped(t *testing.T) {
	body := []byte(`{"data":{"session":{"negotiate":{"result":{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-AAA","extensibilityDisplayName":"Shopify Payments"}}]}}}}}}}`)
	id, gw := extractPaymentID(body)
	if id != "gid-AAA" || gw != "Shopify Payments" {
		t.Errorf("got (%q,%q), want (gid-AAA,Shopify Payments)", id, gw)
	}
}

// Fallback path: fragment without the typed envelope (e.g. seller subtree).
func TestExtractPaymentIDFallback(t *testing.T) {
	body := []byte(`{"sellerProposal":{"payment":{"availablePaymentLines":[{"paymentMethod":{"paymentMethodIdentifier":"gid-BBB","name":"Stripe"}}]}}}`)
	id, gw := extractPaymentID(body)
	if id != "gid-BBB" || gw != "Stripe" {
		t.Errorf("got (%q,%q), want (gid-BBB,Stripe)", id, gw)
	}
}

func TestExtractPaymentIDEmpty(t *testing.T) {
	if id, _ := extractPaymentID([]byte(`{"data":{}}`)); id != "" {
		t.Errorf("got %q, want empty", id)
	}
}

// Write-through must land on the exact nested submit path and never panic.
func TestSetSubmitPaymentID(t *testing.T) {
	sv := map[string]interface{}{
		"input": map[string]interface{}{
			"payment": map[string]interface{}{
				"paymentLines": []map[string]interface{}{{
					"paymentMethod": map[string]interface{}{
						"directPaymentMethod": map[string]interface{}{
							"paymentMethodIdentifier": "gid-OLD",
						},
					},
				}},
			},
		},
	}
	setSubmitPaymentID(sv, "gid-NEW")
	got := sv["input"].(map[string]interface{})["payment"].(map[string]interface{})["paymentLines"].([]map[string]interface{})[0]["paymentMethod"].(map[string]interface{})["directPaymentMethod"].(map[string]interface{})["paymentMethodIdentifier"]
	if got != "gid-NEW" {
		t.Errorf("got %q, want gid-NEW", got)
	}
	// Bad shapes: no-op, no panic.
	setSubmitPaymentID(map[string]interface{}{}, "x")
	setSubmitPaymentID(map[string]interface{}{"input": 1}, "x")
}

// Currency sync must align buyerIdentity and concrete taxes, and skip
// any:true proposal taxes.
func TestSyncProposalCurrency(t *testing.T) {
	v := map[string]interface{}{
		"buyerIdentity": map[string]interface{}{
			"customer": map[string]string{"presentmentCurrency": "USD", "countryCode": "US"},
		},
		"taxes": map[string]interface{}{
			"proposedTotalAmount": map[string]interface{}{"value": map[string]string{"amount": "1", "currencyCode": "USD"}},
		},
	}
	syncProposalCurrency(v, "GBP")
	if c := v["buyerIdentity"].(map[string]interface{})["customer"].(map[string]string)["presentmentCurrency"]; c != "GBP" {
		t.Errorf("buyerIdentity = %q, want GBP", c)
	}
	if c := v["taxes"].(map[string]interface{})["proposedTotalAmount"].(map[string]interface{})["value"].(map[string]string)["currencyCode"]; c != "GBP" {
		t.Errorf("taxes = %q, want GBP", c)
	}
	any := map[string]interface{}{
		"buyerIdentity": map[string]interface{}{
			"customer": map[string]string{"presentmentCurrency": "USD"},
		},
		"taxes": map[string]interface{}{"proposedTotalAmount": map[string]interface{}{"any": true}},
	}
	syncProposalCurrency(any, "GBP") // must not panic
	if c := any["buyerIdentity"].(map[string]interface{})["customer"].(map[string]string)["presentmentCurrency"]; c != "GBP" {
		t.Errorf("buyerIdentity = %q, want GBP", c)
	}
}

// Session-token write-through must land on input.queueToken/checkpointData
// and never panic on bad shapes.
func TestSetSubmitSessionTokens(t *testing.T) {
	sv := map[string]interface{}{
		"input": map[string]interface{}{
			"queueToken": "old-q", "checkpointData": "old-c",
		},
	}
	setSubmitSessionTokens(sv, "new-q", "new-c")
	_in := sv["input"].(map[string]interface{})
	if _in["queueToken"] != "new-q" || _in["checkpointData"] != "new-c" {
		t.Errorf("got %q/%q, want new-q/new-c", _in["queueToken"], _in["checkpointData"])
	}
	setSubmitSessionTokens(map[string]interface{}{}, "q", "c")
	setSubmitSessionTokens(map[string]interface{}{"input": 1}, "q", "c")
}
