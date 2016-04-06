package main

import (
	"../model"
	"flag"
	"fmt"
	"github.com/couchbase/gocb"
	"github.com/gocql/gocql"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	concurrentUpdaters = flag.Int("updaters", 10000, "The number of writer goroutines")
	concurrentReaders  = flag.Int("readers", 10000, "The number of reader goroutines")
	userIdDumpFile     = flag.String("dump_file", "../user_ids_to_test.txt", "Text file containing list of user ids to test")
	cpuprofile         = flag.String("cpuprofile", "./cpustat.prof", "Write cpu profile to given file")
	shouldUseCassandra = flag.Bool("cassandra", false, "Turn on to use Cassandra")

	couchBucket      *gocb.Bucket
	cassandraSession *gocql.Session
	err              error

	wgRead      sync.WaitGroup
	wgUpdate    sync.WaitGroup
	updateCount int64

	userIds []string
)

func benchDbOp(b *testing.B, dbOp func()) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dbOp()
	}
}

func init() {
	flag.Parse()

	// init db connection
	if *shouldUseCassandra {
		cassandraSession, err = initCassandra()
	} else {
		couchBucket, err = initCouchDb()
	}

	if err != nil {
		panic(err)
	}

	//defer cassandraSession.Close()

	// read a list of user ids prepared by db_filler.
	// This is to make sure our benchmarks are evenly spread across user_ids and not optimized for particular user
	userIdBytes, _ := ioutil.ReadFile(*userIdDumpFile)
	userIds = strings.Split(string(userIdBytes), "\n")

	// enable profiling via http
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// record cpu profile to file
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
}

func BenchmarkCouch_Read(b *testing.B) {
	wgRead.Add(*concurrentReaders)
	b.N = *concurrentReaders

	benchDbOp(b, func() {
		defer wgRead.Done()

		userId := randomUserId()
		var err error
		balance := model.Balance{}
		_, err = couchBucket.Get(userId, &balance)

		if err != nil {
			log.Printf("Error during read for key : %v, %v", userId, err)
		}
	})
	wgRead.Wait()
}

func BenchmarkCouch_Update(b *testing.B) {
	wgUpdate.Add(*concurrentUpdaters)
	b.N = *concurrentUpdaters

	benchDbOp(b, func() {
		defer wgUpdate.Done()

		userId := randomUserId()
		balanceKey := fmt.Sprintf("%v_bal", userId)

		var err error
		if updateCount%4 != 0 { // every 4th transaction is a credit
			_, _, err = couchBucket.Counter(balanceKey, -10, 0, 0)
		} else {
			_, _, err = couchBucket.CounterDura(balanceKey, 100, 0, 0, 0, 1)
		}

		if err != nil {
			log.Printf("Error during update for key : %v, %v", balanceKey, err)
		}
		atomic.AddInt64(&updateCount, 1)
	})
	wgUpdate.Wait()
}

func BenchmarkCassandra_Read(b *testing.B) {
	wgRead.Add(*concurrentReaders)
	b.N = *concurrentReaders

	benchDbOp(b, func() {
		defer wgRead.Done()

		userId := randomUserId()
		var balanceVal int64
		err := cassandraSession.Query(`SELECT balance FROM balance_counters WHERE user_id = ? LIMIT 1`,
			userId).Consistency(gocql.One).Scan(&balanceVal)

		if err != nil {
			log.Printf("Error during read for key : %v, %v", userId, err)
		}
	})
	wgRead.Wait()
}

func BenchmarkCassandra_Update(b *testing.B) {
	wgUpdate.Add(*concurrentUpdaters)
	b.N = *concurrentUpdaters

	benchDbOp(b, func() {
		defer wgUpdate.Done()

		userId := randomUserId()
		var delta int64
		if updateCount%4 != 0 { // every 4th transaction is a credit
			delta = 100
		} else {
			delta = -10
		}

		err = cassandraSession.Query(`UPDATE balance_counters SET balance = balance + ? WHERE user_id = ?`,
			delta, userId).Exec()

		if err != nil {
			log.Printf("Error during update for key : %v, %v", userId, err)
		}
		atomic.AddInt64(&updateCount, 1)
	})
	wgUpdate.Wait()
}

func randomUserId() string {
	return strings.TrimSpace(userIds[rand.Intn(len(userIds))])
}

func initCassandra() (*gocql.Session, error) {
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.ProtoVersion = 4
	cluster.Keyspace = "default"
	cluster.Consistency = gocql.One
	return cluster.CreateSession()
}

func initCouchDb() (*gocb.Bucket, error) {
	cluster, _ := gocb.Connect("couchbase://localhost")
	return cluster.OpenBucket("default", "")
}
