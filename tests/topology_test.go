/*
 * Copyright (C) 2015 Red Hat, Inc.
 *
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 *
 */

package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skydive-project/skydive/api/types"
	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/http"
	"github.com/skydive-project/skydive/tests/helper"
	"github.com/skydive-project/skydive/topology"
	"github.com/skydive-project/skydive/topology/graph"
)

func TestBridgeOVS(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ovs-vsctl add-br br-test1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-test1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			gremlin := "g"
			if !c.time.IsZero() {
				gremlin += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin += `.V().Has("Type", "ovsbridge", "Name", "br-test1")`
			gremlin += `.Out("Type", "ovsport", "Name", "br-test1")`
			gremlin += `.Out("Type", "internal", "Name", "br-test1", "Driver", "openvswitch")`

			// we have 2 links between ovsbridge and ovsport, this
			// results in 2 out nodes which are the same node so we Dedup
			gremlin += ".Dedup()"

			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestPatchOVS(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ovs-vsctl add-br br-test1", true},
			{"ovs-vsctl add-br br-test2", true},
			{"ovs-vsctl add-port br-test1 patch-br-test2 -- set interface patch-br-test2 type=patch", true},
			{"ovs-vsctl add-port br-test2 patch-br-test1 -- set interface patch-br-test1 type=patch", true},
			{"ovs-vsctl set interface patch-br-test2 option:peer=patch-br-test1", true},
			{"ovs-vsctl set interface patch-br-test1 option:peer=patch-br-test2", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-test1", true},
			{"ovs-vsctl del-br br-test2", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			gremlin := "g"
			if !c.time.IsZero() {
				gremlin += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin += `.V().Has("Type", "patch", "Name", "patch-br-test1", "Driver", "openvswitch")`
			gremlin += `.Both("Type", "patch", "Name", "patch-br-test2", "Driver", "openvswitch")`

			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			gremlin += `.Dedup()`

			if nodes, err = gh.GetNodes(gremlin); err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestInterfaceOVS(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ovs-vsctl add-br br-test1", true},
			{"ovs-vsctl add-port br-test1 intf1 -- set interface intf1 type=internal", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-test1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin := prefix + `.V().Has("Type", "internal", "Name", "intf1", "Driver", "openvswitch").HasKey("UUID").HasKey("MAC")`
			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected one 'intf1' node with MAC and UUID attributes, got %+v", nodes)
			}

			gremlin = prefix + `.V().Has("Name", "intf1", "Type", Ne("ovsport"))`
			nodes, err = gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected one 'intf1' node with type different than 'ovsport', got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestVeth(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ip l add vm1-veth0 type veth peer name vm1-veth1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ip link del vm1-veth0", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			nodes, err := gh.GetNodes(prefix + `.V().Has("Type", "veth", "Name", "vm1-veth0").Both("Type", "veth", "Name", "vm1-veth1")`)
			if err != nil {
				return err
			}
			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}
			return nil
		}},
	}

	RunTest(t, test)
}

func TestBridge(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"brctl addbr br-test", true},
			{"ip tuntap add mode tap dev intf1", true},
			{"brctl addif br-test intf1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"brctl delbr br-test", true},
			{"ip link del intf1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			nodes, err := gh.GetNodes(prefix + `.V().Has("Type", "bridge", "Name", "br-test").Out("Name", "intf1")`)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestMacNameUpdate(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ip l add vm1-veth0 type veth peer name vm1-veth1", true},
			{"ip l set vm1-veth1 name vm1-veth2", true},
			{"ip l set vm1-veth2 address 00:00:00:00:00:aa", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ip link del vm1-veth0", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			newNodes, err := gh.GetNodes(prefix + `.V().Has("Name", "vm1-veth2", "MAC", "00:00:00:00:00:aa")`)
			if err != nil {
				return err
			}

			oldNodes, err := gh.GetNodes(prefix + `.V().Has("Name", "vm1-veth1")`)
			if err != nil {
				return err
			}

			if len(newNodes) != 1 || len(oldNodes) != 0 {
				return fmt.Errorf("Expected one name named vm1-veth2 and zero named vm1-veth1")
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestNameSpace(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ip netns add ns1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ip netns del ns1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			nodes, err := gh.GetNodes(prefix + `.V().Has("Name", "ns1", "Type", "netns")`)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestNameSpaceVeth(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ip netns add ns1", true},
			{"ip l add vm1-veth0 type veth peer name vm1-veth1 netns ns1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ip link del vm1-veth0", true},
			{"ip netns del ns1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			nodes, err := gh.GetNodes(prefix + `.V().Has("Name", "ns1", "Type", "netns").Out("Name", "vm1-veth1", "Type", "veth")`)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestNameSpaceOVSInterface(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ip netns add ns1", true},
			{"ovs-vsctl add-br br-test1", true},
			{"ovs-vsctl add-port br-test1 intf1 -- set interface intf1 type=internal", true},
			{"ip l set intf1 netns ns1", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-test1", true},
			{"ip netns del ns1", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			nodes, err := gh.GetNodes(prefix + `.V().Has("Name", "ns1", "Type", "netns").Out("Name", "intf1", "Type", "internal")`)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node of type internal, got %+v", nodes)
			}

			nodes, err = gh.GetNodes(prefix + `.V().Has("Name", "intf1", "Type", "internal")`)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestDockerSimple(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"docker run -d -t -i --name test-skydive-docker-simple busybox", false},
		},

		tearDownCmds: []helper.Cmd{
			{"docker rm -f test-skydive-docker-simple", false},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			gremlin := "g"
			if !c.time.IsZero() {
				gremlin += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin += `.V().Has("Type", "netns", "Manager", "docker")`
			gremlin += `.Out("Type", "container", "Docker.ContainerName", "/test-skydive-docker-simple")`

			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 node, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestDockerShareNamespace(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"docker run -d -t -i --name test-skydive-docker-share-ns busybox", false},
			{"docker run -d -t -i --name test-skydive-docker-share-ns2 --net=container:test-skydive-docker-share-ns busybox", false},
		},

		tearDownCmds: []helper.Cmd{
			{"docker rm -f test-skydive-docker-share-ns", false},
			{"docker rm -f test-skydive-docker-share-ns2", false},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			gremlin := "g"
			if !c.time.IsZero() {
				gremlin += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin += `.V().Has("Type", "netns", "Manager", "docker")`
			gremlin += `.Out().Has("Type", "container", "Docker.ContainerName", Within("/test-skydive-docker-share-ns", "/test-skydive-docker-share-ns2"))`
			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 2 {
				return fmt.Errorf("Expected 2 nodes, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestDockerNetHost(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"docker run -d -t -i --net=host --name test-skydive-docker-net-host busybox", false},
		},

		tearDownCmds: []helper.Cmd{
			{"docker rm -f test-skydive-docker-net-host", false},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin := prefix + `.V().Has("Docker.ContainerName", "/test-skydive-docker-net-host", "Type", "container")`
			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 1 {
				return fmt.Errorf("Expected 1 container, got %+v", nodes)
			}

			gremlin = prefix + `.V().Has("Type", "netns", "Manager", "docker", "Name", "test-skydive-docker-net-host")`
			nodes, err = gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) != 0 {
				return fmt.Errorf("There should be only no namespace managed by Docker, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestDockerLabels(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"docker run -d -t -i --label a.b.c=123 --label a~b/c@d=456 --name test-skydive-docker-labels busybox", false},
		},

		tearDownCmds: []helper.Cmd{
			{"docker rm -f test-skydive-docker-labels", false},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			gremlin := prefix + `.V().Has("Docker.ContainerName", "/test-skydive-docker-labels",`
			gremlin += ` "Type", "container", "Docker.Labels.a.b.c", "123", "Docker.Labels.a~b/c@d", "456")`
			fmt.Printf("Gremlin: %s\n", gremlin)
			_, err := gh.GetNode(gremlin)
			if err != nil {
				return err
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestInterfaceUpdate(t *testing.T) {
	start := time.Now()

	test := &Test{
		mode: OneShot,
		setupCmds: []helper.Cmd{
			{"ip netns add iu", true},
			{"sleep 5", false},
			{"ip netns exec iu ip link set lo up", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ip netns del iu", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			now := time.Now()
			gremlin := fmt.Sprintf("g.Context(%d, %d)", common.UnixMillis(now), int(now.Sub(start).Seconds()))
			gremlin += `.V().Has("Name", "iu", "Type", "netns").Out().Has("Name", "lo")`

			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			if len(nodes) < 2 {
				return fmt.Errorf("Expected at least 2 nodes, got %+v", nodes)
			}

			hasDown := false
			hasUp := false
			for i := range nodes {
				if !hasDown && nodes[i].Metadata()["State"].(string) == "DOWN" {
					hasDown = true
				}
				if !hasUp && nodes[i].Metadata()["State"].(string) == "UP" {
					hasUp = true
				}
			}

			if !hasUp || !hasDown {
				return fmt.Errorf("Expected one node up and one node down, got %+v", nodes)
			}

			return nil
		}},
	}

	RunTest(t, test)
}

func TestInterfaceMetrics(t *testing.T) {
	test := &Test{
		mode: OneShot,
		setupCmds: []helper.Cmd{
			{"ip netns add im", true},
			{"ip netns exec im ip link set lo up", true},
			{"sleep 2", false},
		},

		setupFunction: func(c *TestContext) error {
			helper.ExecCmds(t,
				helper.Cmd{Cmd: "ip netns exec im ping -c 15 127.0.0.1", Check: true},
				helper.Cmd{Cmd: "sleep 5", Check: false},
			)
			return nil
		},

		tearDownCmds: []helper.Cmd{
			{"ip netns del im", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			gremlin := fmt.Sprintf("g.Context(%d, %d)", common.UnixMillis(c.startTime), c.startTime.Unix()-c.setupTime.Unix()+5)
			gremlin += `.V().Has("Name", "im", "Type", "netns").Out().Has("Name", "lo").Metrics().Aggregates()`

			metrics, err := gh.GetMetrics(gremlin)
			if err != nil {
				return err
			}

			if len(metrics) != 1 {
				return fmt.Errorf("Expected one aggregated metric, got %+v", metrics)
			}

			if len(metrics["Aggregated"]) <= 1 {
				return fmt.Errorf("Should have more metrics entry, got %+v", metrics["Aggregated"])
			}

			var start, tx int64
			for _, m := range metrics["Aggregated"] {
				if m.GetStart() < start {
					j, _ := json.MarshalIndent(metrics, "", "\t")
					return fmt.Errorf("Metrics not correctly sorted (%+v)", string(j))
				}
				start = m.GetStart()

				im := m.(*topology.InterfaceMetric)
				tx += im.TxPackets
			}

			if tx != 30 {
				return fmt.Errorf("Expected 30 TxPackets, got %d", tx)
			}

			gremlin += `.Sum()`

			m, err := gh.GetMetric(gremlin)
			if err != nil {
				return fmt.Errorf("Could not find metrics with: %s", gremlin)
			}

			im := m.(*topology.InterfaceMetric)
			if im.TxPackets != 30 {
				return fmt.Errorf("Expected 30 TxPackets, got %d", tx)
			}

			return nil
		}},
	}

	RunTest(t, test)
}
func TestOVSOwnershipLink(t *testing.T) {
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ovs-vsctl add-br br-owner", true},
			{"ovs-vsctl add-port br-owner patch-br-owner -- set interface patch-br-owner type=patch", true},
			{"ovs-vsctl add-port br-owner gre-br-owner -- set interface gre-br-owner type=gre", true},
			{"ovs-vsctl add-port br-owner vxlan-br-owner -- set interface vxlan-br-owner type=vxlan", true},
			{"ovs-vsctl add-port br-owner geneve-br-owner -- set interface geneve-br-owner type=geneve", true},
			{"ovs-vsctl add-port br-owner intf-owner -- set interface intf-owner type=internal", true},
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-owner", true},
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh
			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			intfs := []string{"patch-br-owner", "gre-br-owner", "vxlan-br-owner", "geneve-br-owner"}
			for _, intf := range intfs {
				gremlin := prefix + fmt.Sprintf(`.V().Has('Name', '%s', 'Type', NE('ovsport')).InE().Has('RelationType', 'ownership').InV().Has('Name', 'br-owner')`, intf)
				nodes, err := gh.GetNodes(gremlin)
				if err != nil {
					return err
				}

				// only the host node shouldn't have a parent ownership link
				if len(nodes) != 1 {
					return errors.New("tunneling and patch interface should have one ownership link to the bridge")
				}
			}

			gremlin := prefix + `.V().Has('Name', 'intf-owner', 'Type', NE('ovsport')).InE().Has('RelationType', 'ownership').InV().Has('Type', 'host')`
			nodes, err := gh.GetNodes(gremlin)
			if err != nil {
				return err
			}

			// only the host node shouldn't have a parent ownership link
			if len(nodes) != 1 {
				return errors.New("internal interface should have one ownership link to the host")
			}

			return nil
		}},
	}

	RunTest(t, test)
}

type TopologyInjecter struct {
	shttp.DefaultWSSpeakerEventHandler
	connected int32
}

func (t *TopologyInjecter) OnConnected(c shttp.WSSpeaker) {
	atomic.StoreInt32(&t.connected, 1)
}

func TestQueryMetadata(t *testing.T) {
	test := &Test{
		setupFunction: func(c *TestContext) error {
			authOptions := &shttp.AuthenticationOpts{}
			addresses, err := config.GetAnalyzerServiceAddresses()
			if err != nil || len(addresses) == 0 {
				return fmt.Errorf("Unable to get the analyzers list: %s", err.Error())
			}

			hostname, _ := os.Hostname()
			wspool := shttp.NewWSJSONClientPool("TestQueryMetadata")
			for _, sa := range addresses {
				authClient := shttp.NewAuthenticationClient(config.GetURL("http", sa.Addr, sa.Port, ""), authOptions)
				client := shttp.NewWSClient(hostname+"-cli", common.UnknownService, config.GetURL("ws", sa.Addr, sa.Port, "/ws/publisher"), authClient, http.Header{}, 1000)
				wspool.AddClient(client)
			}

			masterElection := shttp.NewWSMasterElection(wspool)

			eventHandler := &TopologyInjecter{}
			wspool.AddEventHandler(eventHandler)
			wspool.ConnectAll()

			err = common.Retry(func() error {
				if atomic.LoadInt32(&eventHandler.connected) != 1 {
					return errors.New("Not connected through WebSocket")
				}
				return nil
			}, 10, time.Second)

			if err != nil {
				return err
			}

			n := new(graph.Node)
			n.Decode(map[string]interface{}{
				"ID":   "123",
				"Host": "test",
				"Metadata": map[string]interface{}{
					"A": map[string]interface{}{
						"B": map[string]interface{}{
							"C": 123,
							"D": []interface{}{1, 2, 3},
						},
						"F": map[string]interface{}{
							"G": 123,
						},
					},
				},
			})

			msg := shttp.NewWSJSONMessage(graph.Namespace, graph.NodeAddedMsgType, n)
			masterElection.SendMessageToMaster(msg)

			return nil
		},

		checks: []CheckFunction{func(c *CheckContext) error {
			gh := c.gh

			prefix := "g"
			if !c.time.IsZero() {
				prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
			}

			_, err := gh.GetNode(prefix + `.V().Has("A.F.G", 123)`)
			if err != nil {
				return err
			}

			_, err = gh.GetNode(prefix + `.V().Has("A.B.C", 123)`)
			if err != nil {
				return err
			}

			_, err = gh.GetNode(prefix + `.V().Has("A.B.D", 1)`)
			if err != nil {
				return err
			}

			return nil
		}},
	}

	RunTest(t, test)
}

//TestUserMetadata tests user metadata functionality
func TestUserMetadata(t *testing.T) {
	umd := types.NewUserMetadata("G.V().Has('Name', 'br-umd', 'Type', 'ovsbridge')", "testKey", "testValue")
	test := &Test{
		setupCmds: []helper.Cmd{
			{"ovs-vsctl add-br br-umd", true},
		},

		setupFunction: func(c *TestContext) error {
			return c.client.Create("usermetadata", umd)
		},

		tearDownFunction: func(c *TestContext) error {
			c.client.Delete("usermetadata", umd.ID())
			return nil
		},

		tearDownCmds: []helper.Cmd{
			{"ovs-vsctl del-br br-umd", true},
		},

		checks: []CheckFunction{
			func(c *CheckContext) error {
				prefix := "g"
				if !c.time.IsZero() {
					prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
				}

				_, err := c.gh.GetNode(prefix + ".V().Has('UserMetadata.testKey', 'testValue')")
				if err != nil {
					return fmt.Errorf("Failed to find a node with UserMetadata.testKey metadata")
				}

				return err
			},

			func(c *CheckContext) error {
				prefix := "g"
				if !c.time.IsZero() {
					prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
				}

				c.client.Delete("usermetadata", umd.ID())

				node, err := c.gh.GetNode(prefix + ".V().Has('UserMetadata.testKey', 'testValue')")
				if err != common.ErrNotFound {
					return fmt.Errorf("Node %+v was found with metadata UserMetadata.testKey", node)
				}

				return nil
			},
		},
	}

	RunTest(t, test)
}

// TestAgentMetadata tests metadata set to the agent using the configuration file
func TestAgentMetadata(t *testing.T) {
	test := &Test{
		checks: []CheckFunction{
			func(c *CheckContext) error {
				prefix := "g"
				if !c.time.IsZero() {
					prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
				}

				_, err := c.gh.GetNode(prefix + ".V().Has('mydict.value', 123)")
				if err != nil {
					return fmt.Errorf("Failed to find the host node with mydict.value metadata")
				}

				return nil
			},
		},
	}

	RunTest(t, test)
}

//TestRouteTable tests route table update
func TestRouteTable(t *testing.T) {
	gopath := os.Getenv("GOPATH")
	topology := gopath + "/src/github.com/skydive-project/skydive/scripts/simple.sh"

	test := &Test{
		mode: OneShot,

		setupCmds: []helper.Cmd{
			{fmt.Sprintf("%s start 124.65.91.42/24 124.65.92.43/24", topology), true},
		},

		tearDownCmds: []helper.Cmd{
			{fmt.Sprintf("%s stop", topology), true},
		},

		checks: []CheckFunction{
			func(c *CheckContext) error {
				prefix := "g"
				if !c.time.IsZero() {
					prefix += fmt.Sprintf(".Context(%d)", common.UnixMillis(c.time))
				}

				node, err := c.gh.GetNode(prefix + ".V().Has('IPV4', '124.65.91.42/24')")
				if err != nil {
					return fmt.Errorf("Failed to find a node with IP 124.65.91.42/24")
				}

				routingTable := node.Metadata()["RoutingTable"].([]interface{})
				noOfRoutingTable := len(routingTable)

				helper.ExecCmds(t,
					helper.Cmd{Cmd: "ip netns exec vm1 ip route add 124.65.92.0/24 via 124.65.91.42 table 2", Check: true},
					helper.Cmd{Cmd: "sleep 5", Check: false},
				)

				node, err = c.gh.GetNode(prefix + ".V().Has('IPV4', '124.65.91.42/24')")
				routingTable = node.Metadata()["RoutingTable"].([]interface{})
				newNoOfRoutingTable := len(routingTable)

				helper.ExecCmds(t,
					helper.Cmd{Cmd: "ip netns exec vm1 ip route del 124.65.92.0/24 via 124.65.91.42 table 2", Check: true},
					helper.Cmd{Cmd: "sleep 5", Check: false},
				)
				if newNoOfRoutingTable <= noOfRoutingTable {
					return fmt.Errorf("Failed to add Route")
				}
				return nil
			},
		},
	}
	RunTest(t, test)
}

//TestRouteTableHistory tests route table update available in history
func TestRouteTableHistory(t *testing.T) {
	gopath := os.Getenv("GOPATH")
	topology := gopath + "/src/github.com/skydive-project/skydive/scripts/simple.sh"

	test := &Test{
		mode: OneShot,

		setupCmds: []helper.Cmd{
			{fmt.Sprintf("%s start 124.65.75.42/24 124.65.76.43/24", topology), true},
			{"sleep 5", false},
			{"ip netns exec vm1 ip route add 124.65.75.0/24 via 124.65.75.42 table 2", true},
		},

		tearDownCmds: []helper.Cmd{
			{fmt.Sprintf("%s stop", topology), true},
		},

		checks: []CheckFunction{
			func(c *CheckContext) error {
				prefix := fmt.Sprintf("g.Context(%d)", common.UnixMillis(time.Now()))
				node, err := c.gh.GetNode(prefix + ".V().Has('IPV4', '124.65.75.42/24')")
				if err != nil {
					return fmt.Errorf("Failed to find a node with IP 124.65.75.42/24")
				}
				routingTable := node.Metadata()["RoutingTable"].([]interface{})
				foundNewTable := false
				for _, obj := range routingTable {
					rt := obj.(map[string]interface{})
					if (rt["Id"].(json.Number)).String() == "2" {
						foundNewTable = true
						break
					}
				}
				if !foundNewTable {
					return fmt.Errorf("Failed to get added Route from history")
				}
				return nil
			},
		},
	}
	RunTest(t, test)
}
