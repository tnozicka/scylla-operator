// Copyright (c) 2021 ScyllaDB

package scyllacluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/table"
	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/test/e2e/framework"
	"github.com/scylladb/scylla-operator/test/e2e/utils"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const nRows = 10

type DataInserter struct {
	session           *gocqlx.Session
	keyspace          string
	table             *table.Table
	data              []*TestData
	replicationFactor int32
}

type TestData struct {
	Id   int    `db:"id"`
	Data string `db:"data"`
}

func NewDataInserter(replicationFactor int32) (*DataInserter, error) {
	if replicationFactor < 1 {
		return nil, fmt.Errorf("replication factor can't be set to less than 1")
	}

	keyspace := strings.Replace(string(uuid.NewUUID()), "-", "", -1)
	table := table.New(table.Metadata{
		Name:    fmt.Sprintf(`"%s"."test"`, keyspace),
		Columns: []string{"id", "data"},
		PartKey: []string{"id"},
	})
	data := make([]*TestData, 0, nRows)
	for i := 0; i < nRows; i++ {
		data = append(data, &TestData{Id: i, Data: rand.String(32)})
	}

	di := &DataInserter{
		keyspace:          keyspace,
		table:             table,
		data:              data,
		replicationFactor: replicationFactor,
	}

	return di, nil
}

func (di *DataInserter) Close() {
	if di.session != nil {
		di.session.Close()
	}
}

// SetClientEndpointsAndWaitForConsistencyAll create a new session and closes a previous session if it existed.
// It will wait for scylla to reach consistency ALL.
// In case an error was returned, DataInserter can no Longer be used.
func (di *DataInserter) SetClientEndpointsAndWaitForConsistencyAll(ctx context.Context, client corev1client.CoreV1Interface, sc *scyllav1.ScyllaCluster) error {
	di.Close()

	scyllaClient, hosts, err := utils.GetScyllaClient(ctx, client, sc)
	if err != nil {
		return fmt.Errorf("can't get scylla client: %w", err)
	}
	defer scyllaClient.Close()

	// Unfortunately, Gossip status propagation can take well over a minute, so we need to set a large timeout.
	err = wait.PollImmediateWithContext(ctx, 5*time.Minute, time.Second, func(ctx context.Context) (done bool, err error) {
		allSeeAllAsUN := true
		for _, h := range hosts {
			s, err := scyllaClient.Status(ctx, h)
			if err != nil {
				return true, fmt.Errorf("can't get scylla status on node %q: %w", h, err)
			}

			downHosts := s.DownHosts()
			framework.Infof("Node %q, down: %q, up: %q", h, strings.Join(downHosts, ","), strings.Join(s.LiveHosts(), ","))

			if len(downHosts) != 0 {
				allSeeAllAsUN = false
			}
		}

		return allSeeAllAsUN, nil
	})
	if err != nil {
		return fmt.Errorf("can't wait for nodes to reach consistency ALL: %w", err)
	}

	di.session, err = di.createSession(hosts)
	if err != nil {
		return fmt.Errorf("can't create session: %w", err)
	}

	return nil
}

func (di *DataInserter) Insert() error {
	framework.By("Inserting data with RF=%d", di.replicationFactor)

	err := di.session.ExecStmt(fmt.Sprintf(`CREATE KEYSPACE "%s" WITH replication = {'class' : 'NetworkTopologyStrategy','replication_factor' : %d}`, di.keyspace, di.replicationFactor))
	if err != nil {
		return fmt.Errorf("can't create keyspace: %w", err)
	}

	err = di.session.ExecStmt(fmt.Sprintf("CREATE TABLE %s (id int primary key, data text)", di.table.Name()))
	if err != nil {
		return fmt.Errorf("can't create table: %w", err)
	}

	for _, t := range di.data {
		q := di.session.Query(di.table.Insert()).BindStruct(t)
		err = q.ExecRelease()
		if err != nil {
			return fmt.Errorf("can't insert data: %w", err)
		}
	}

	return nil
}

func (di *DataInserter) Read() ([]*TestData, error) {
	framework.By("Reading data with RF=%d", di.replicationFactor)

	di.session.SetConsistency(gocql.All)

	var res []*TestData
	q := di.session.Query(di.table.SelectAll()).BindStruct(&TestData{})
	err := q.SelectRelease(&res)
	if err != nil {
		return nil, fmt.Errorf("can't select data: %w", err)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Id < res[j].Id
	})

	return res, nil
}

func (di *DataInserter) GetExpected() []*TestData {
	return di.data
}

func (di *DataInserter) createSession(hosts []string) (*gocqlx.Session, error) {
	clusterConfig := gocql.NewCluster(hosts...)
	clusterConfig.Timeout = 3 * time.Second
	clusterConfig.ConnectTimeout = 3 * time.Second

	session, err := gocqlx.WrapSession(clusterConfig.CreateSession())
	if err != nil {
		return nil, fmt.Errorf("can't create gocqlx session: %w", err)
	}

	return &session, nil
}
