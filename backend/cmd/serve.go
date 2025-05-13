/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"strconv"
	"strings"

	"github.com/ipfs/boxo/bitswap/network/bsnet"
	bsserver "github.com/ipfs/boxo/bitswap/server"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	chunker "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	uih "github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dsync "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multicodec"
	"github.com/spf13/cobra"
)

var (
	seedF int64
)

const exampleBinaryName = "bitswap-transfer"

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("serve called")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// For this example we are going to be transferring data using Bitswap over libp2p
		// This means we need to create a libp2p host first

		// Make a host that listens on the given multiaddress
		h, err := makeHost(0, seedF)
		if err != nil {
			log.Fatal(err)
		}
		defer h.Close()

		fullAddr := getHostAddress(h)
		log.Printf("I am %s\n", fullAddr)

		c, bs, err := startDataServer(ctx, h)
		if err != nil {
			log.Fatal(err)
		}
		defer bs.Close()
		log.Printf("hosting UnixFS file with CID: %s\n", c)
		log.Println("listening for inbound connections and Bitswap requests")
		log.Printf("Now run \"./%s get -d %s\" on a different terminal\n", exampleBinaryName, fullAddr)

		// Run until canceled.
		<-ctx.Done()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	serveCmd.Flags().Int64VarP(&seedF, "seed", "s", 0, "The seed to use for the host")
}

// makeHost creates a libP2P host with a random peer ID listening on the
// given multiaddress.
func makeHost(listenPort int, randseed int64) (host.Host, error) {
	var r io.Reader
	if randseed == 0 {
		r = rand.Reader
	} else {
		r = mrand.New(mrand.NewSource(randseed))
	}

	// Generate a key pair for this host. We will use it at least
	// to obtain a valid host ID.
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		return nil, err
	}

	// Some basic libp2p options, see the go-libp2p docs for more details
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort)), // port we are listening on, limiting to a single interface and protocol for simplicity
		libp2p.Identity(priv),
	}

	return libp2p.New(opts...)
}

func getHostAddress(h host.Host) string {
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", h.ID().String()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	addr := h.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

// The CID of the file with the number 0 to 100k, built with the parameters:
// CIDv1 links, a 256bit sha2-256 hash function, raw-leaves, a balanced layout, 256kiB chunks, and 174 max links per block
const fileCid = "bafybeiecq2irw4fl5vunnxo6cegoutv4de63h7n27tekkjtak3jrvrzzhe"

// createFile0to100k creates a file with the number 0 to 100k
func createFile0to100k() ([]byte, error) {
	b := strings.Builder{}
	for i := 0; i <= 100000; i++ {
		s := strconv.Itoa(i)
		_, err := b.WriteString(s)
		if err != nil {
			return nil, err
		}
	}
	return []byte(b.String()), nil
}

func startDataServer(ctx context.Context, h host.Host) (cid.Cid, *bsserver.Server, error) {
	fileBytes, err := createFile0to100k()
	if err != nil {
		return cid.Undef, nil, err
	}
	fileReader := bytes.NewReader(fileBytes)

	ds := dsync.MutexWrap(datastore.NewMapDatastore())
	bs := blockstore.NewBlockstore(ds)
	bs = blockstore.NewIdStore(bs) // handle identity multihashes, these don't require doing any actual lookups

	bsrv := blockservice.New(bs, offline.Exchange(bs))
	dsrv := merkledag.NewDAGService(bsrv)

	// Create a UnixFS graph from our file, parameters described here but can be visualized at https://dag.ipfs.tech/
	ufsImportParams := uih.DagBuilderParams{
		Maxlinks:  uih.DefaultLinksPerBlock, // Default max of 174 links per block
		RawLeaves: true,                     // Leave the actual file bytes untouched instead of wrapping them in a dag-pb protobuf wrapper
		CidBuilder: cid.V1Builder{ // Use CIDv1 for all links
			Codec:    uint64(multicodec.DagPb),
			MhType:   uint64(multicodec.Sha2_256), // Use SHA2-256 as the hash function
			MhLength: -1,                          // Use the default hash length for the given hash function (in this case 256 bits)
		},
		Dagserv: dsrv,
		NoCopy:  false,
	}
	ufsBuilder, err := ufsImportParams.New(chunker.NewSizeSplitter(fileReader, chunker.DefaultBlockSize)) // Split the file up into fixed sized 256KiB chunks
	if err != nil {
		return cid.Undef, nil, err
	}
	nd, err := balanced.Layout(ufsBuilder) // Arrange the graph with a balanced layout
	if err != nil {
		return cid.Undef, nil, err
	}

	// Start listening on the Bitswap protocol
	// For this example we're not leveraging any content routing (DHT, IPNI, delegated routing requests, etc.) as we know the peer we are fetching from
	n := bsnet.NewFromIpfsHost(h)
	bswap := bsserver.New(ctx, n, bs)
	n.Start(bswap)
	return nd.Cid(), bswap, nil
}
