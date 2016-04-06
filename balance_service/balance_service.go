package main

import (
	"../model"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"flag"
	"github.com/couchbaselabs/gocb"
	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
)

const COUCHDB_PERSIST_TO = 1   // no. of clusters to persist for durability ops. In production, should represent no of available clusters
const COUCHDB_REPLICATE_TO = 0 // no. of clusters to replicate to, before durability ops is considered successful

var (
	shouldUseCassandra = flag.Bool("cassandra", false, "Turn on to use Cassandra")
	couchBucket        *gocb.Bucket
	cassandraSession   *gocql.Session
	err                error
)

func main() {
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

	defer cassandraSession.Close()

	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello! Try GET, PATCH /balance/:userId queries instead!")
	})

	r.GET("/balance/:userId", func(c *gin.Context) {
		userId := c.Param("userId")

		var balance *model.Balance
		if *shouldUseCassandra {
			balance, err = readCassandra(userId)
		} else {
			balance, err = readCouch(userId)
		}

		if err != nil {
			handleDbErr(err, c)
		} else {
			c.JSON(http.StatusOK, gin.H{"status": "success", "data": balance})
		}
	})

	/*
			For updating balance

		 	PATCH /balance/<userId> HTTP/1.1
		 	Content-Type: application/x-www-form-urlencoded

			delta=-1000
	*/
	r.PATCH("/balance/:userId", func(c *gin.Context) {
		userId := c.Param("userId")
		deltaStr := c.PostForm("delta")

		delta, _ := strconv.ParseInt(deltaStr, 10, 64)

		if *shouldUseCassandra {
			err = writeCassandra(userId, delta)
		} else {
			err = writeCouch(userId, delta)
		}

		if err != nil {
			handleDbErr(err, c)
		} else {
			c.String(http.StatusOK, "success")
		}
	})

	r.Run(":8080")
}

func writeCouch(userId string, delta int64) error {
	balance := model.Balance{}

	_, err := couchBucket.Get(userId, &balance)
	if err != nil {
		return err
	}

	if delta > 0 {
		// use durable to make sure value is replicated across clusters/nodes
		// to speed durable ops, we need faster disks(SSD) and faster network
		_, _, err = couchBucket.CounterDura(balance.BalanceRef, delta, 0, 0, COUCHDB_REPLICATE_TO, COUCHDB_PERSIST_TO)
	} else {
		// for debit use local atomic ops
		_, _, err = couchBucket.Counter(balance.BalanceRef, delta, 0, 0)
	}
	// maybe update other fields in balance doc

	return err
}

func writeCassandra(userId string, delta int64) error {
	err := cassandraSession.Query(`UPDATE balance_counters SET balance = balance + ? WHERE user_id = ?`, delta, userId).
		Exec()

	return err
}

func readCouch(userId string) (*model.Balance, error) {

	balance := model.Balance{}

	// fetch
	// ideally use a key that provides consistent hashing so we are not off-balancing clusters
	// using userId for simplicity's sake
	_, err := couchBucket.Get(userId, &balance)
	if err != nil {
		return nil, err
	}

	// fetch the actual balance field from key
	var balanceVal int64
	_, err = couchBucket.Get(balance.BalanceRef, &balanceVal)
	if err != nil {
		return nil, err
	}

	balance.Balance = balanceVal

	return &balance, nil
}

func readCassandra(userId string) (*model.Balance, error) {

	balance := model.Balance{}

	// fetch
	var balanceVal int64
	err := cassandraSession.Query(`SELECT balance FROM balance_counters WHERE user_id = ? LIMIT 1`,
		userId).Consistency(gocql.One).Scan(&balanceVal)
	if err != nil {
		return nil, err
	}

	balance.UserId = userId
	balance.Balance = balanceVal

	return &balance, nil
}

func handleDbErr(err error, c *gin.Context) {
	if err != nil {
		if strings.Contains(err.Error(), "KEY_ENOENT") {
			c.AbortWithError(http.StatusNotFound, errors.New("User not found!"))
			panic(err) // bleh.. In production, should just pass to middleware or stop current handler. gin currently doesn't stop the current handler
		}
		c.AbortWithError(http.StatusInternalServerError, err)
		panic(err) // bleh.. In production, should just pass to middleware or stop current handler. gin doesn't stop the current handler
	}
}

func initCassandra() (*gocql.Session, error) {
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.ProtoVersion = 4
	cluster.Keyspace = "default"
	cluster.Consistency = gocql.One // since we have only one local instance to test
	return cluster.CreateSession()
}

func initCouchDb() (*gocb.Bucket, error) {
	cluster, _ := gocb.Connect("couchbase://localhost")
	return cluster.OpenBucket("default", "")
}
