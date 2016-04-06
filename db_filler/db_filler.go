package main

import (
	"../model"
	"flag"
	"fmt"
	"github.com/couchbaselabs/gocb"
	"github.com/gocql/gocql"
	"github.com/nu7hatch/gouuid"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

var recordsToFill = flag.Int("records", 10000, "Records to fill")
var userIdDumpFile = flag.String("dump", "../user_ids_to_test.txt", "File to dump generated user ids to")
var shouldDumpCSV = flag.Bool("csv", false, "Turn on to dump CSV file for POSTMAN")
var shouldUseCassandra = flag.Bool("cassandra", false, "Turn on to use Cassandra")
var recordCounter int64

func main() {
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(*recordsToFill)

	// dump the user ids to a text file that can be used by test scripts
	f, _ := os.OpenFile(*userIdDumpFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	defer f.Close()

	if *shouldDumpCSV {
		f.WriteString("uid,delta" + "\n")
	}

	for i := 0; i < *recordsToFill; i++ {
		if *shouldUseCassandra {
			cassandraFill(&wg, f)
		} else {
			couchDbFill(&wg, f)
		}
	}

	wg.Wait()
}

func couchDbFill(wg *sync.WaitGroup, f *os.File) {
	defer wg.Done()

	cluster, _ := gocb.Connect("couchbase://localhost")
	balanceDb, err := cluster.OpenBucket("default", "")
	if err != nil {
		fmt.Errorf("Failed to get bucket from couchbase (%s)\n", err)
	}

	balance := model.Balance{}

	uid, _ := uuid.NewV4()
	balance.UserId = uid.String()
	balance.BalanceRef = balance.UserId + "_bal"

	_, err = balanceDb.Upsert(balance.UserId, &balance, 0)
	if err != nil {
		fmt.Errorf("Failed tp insert - %s", err)
	}

	_, err = balanceDb.Upsert(balance.BalanceRef, 1000, 0)
	if err != nil {
		fmt.Errorf("Failed tp insert - %s", err)
	} else {
		if *shouldDumpCSV {
			f.WriteString(balance.UserId + "," + creditOrDebit() + "\n")
		} else {
			f.WriteString(balance.UserId + "\n")
		}
	}
}

func cassandraFill(wg *sync.WaitGroup, f *os.File) {
	defer wg.Done()

	cluster := gocql.NewCluster("127.0.0.1")
	cluster.ProtoVersion = 4
	cluster.Keyspace = "default"
	cluster.Consistency = gocql.One
	session, _ := cluster.CreateSession()
	//defer session.Close()

	// insert balance
	uuid, _ := gocql.RandomUUID()
	userId := uuid.String()
	err := session.Query(`INSERT INTO balance (user_id) VALUES (?)`, userId).Exec()
	if err == nil {
		err = session.Query(`UPDATE balance_counters SET balance = balance + ? WHERE user_id = ?`, 1000, userId).Exec()
	}

	if err != nil {
		fmt.Errorf("Failed tp insert - %s", err)
	} else {
		if *shouldDumpCSV {
			f.WriteString(userId + "," + creditOrDebit() + "\n")
		} else {
			f.WriteString(userId + "\n")
		}
	}
}

func creditOrDebit() string {
	atomic.AddInt64(&recordCounter, 1)

	if recordCounter%4 == 0 {
		return strconv.Itoa(10)
	} else {
		return strconv.Itoa(-10)
	}
}
