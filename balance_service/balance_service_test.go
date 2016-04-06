package main

// NOTE: the rest service should be running before these tests
import (
	"../model"
	"github.com/couchbase/gocb"
	"github.com/parnurzeal/gorequest"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	CreateTestData()

	req := gorequest.New()
	_, body, err := req.Get("http://localhost:8080/balance/test-user").End()
	if err != nil {
		t.Error("Get request failed!")
	}

	if body == "" {
		t.Error("Get request body empty!")
	}

	if !strings.Contains(body, "\"userId\":\"test-user\"") {
		t.Error("User Id in Get request mismatch")
	}

	if !strings.Contains(body, "\"balance\":1000") {
		t.Error("Balance in Get request mismatch")
	}
}

func TestCreditPatch(t *testing.T) {
	CreateTestData()

	req := gorequest.New()
	_, body, err := req.Patch("http://localhost:8080/balance/test-user").
		Send("delta=100").
		End()

	if err != nil {
		t.Errorf("Patch credit request failed, with error: %s", err)
	}

	if body == "" {
		t.Error("Patch credit request body empty!")
	}

	if !strings.Contains(body, "success") {
		t.Errorf("Patch credit request failed, response: %s", body)
	}

	// make sure credit amount is reflected
	req = gorequest.New()
	_, body, _ = req.Get("http://localhost:8080/balance/test-user").End()
	if !strings.Contains(body, "\"balance\":1100") {
		t.Errorf("Balance not reflected with credit, response: %s", body)
	}
}

func TestDebitPatch(t *testing.T) {
	CreateTestData()

	req := gorequest.New()
	_, body, err := req.Patch("http://localhost:8080/balance/test-user").
		Send("delta=-10").
		End()

	if err != nil {
		t.Errorf("Patch debit request failed, with error: %s", err)
	}

	if body == "" {
		t.Error("Patch debit request body empty!")
	}

	if !strings.Contains(body, "success") {
		t.Errorf("Patch debit request failed, response: %s", body)
	}

	// make sure debit amount is reflected
	req = gorequest.New()
	_, body, _ = req.Get("http://localhost:8080/balance/test-user").End()
	if !strings.Contains(body, "\"balance\":990") {
		t.Errorf("Balance not reflected with debit, response: %s", body)
	}
}

func CreateTestData() {
	cluster, _ := gocb.Connect("couchbase://localhost")
	balanceDb, _ := cluster.OpenBucket("default", "")

	balance := model.Balance{}

	balance.UserId = "test-user"
	balance.BalanceRef = balance.UserId + "_bal"

	balanceDb.Upsert(balance.UserId, &balance, 0)
	balanceDb.Upsert(balance.BalanceRef, 1000, 0)
}
