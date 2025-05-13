/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	bsclient "github.com/ipfs/boxo/bitswap/client"
	"github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/blockstore"
	"github.com/ipfs/boxo/files"
	"github.com/ipfs/boxo/ipld/merkledag"
	unixfile "github.com/ipfs/boxo/ipld/unixfs/file"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	targetF string
)

// getCmd represents the get command
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("get called")

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

		log.Printf("downloading UnixFS file with CID: %s\n", fileCid)
		fileData, err := runClient(ctx, h, cid.MustParse(fileCid), targetF)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("found the data")
		log.Println(string(fileData))
		log.Println("the file was all the numbers from 0 to 100k!")
	},
}

func init() {
	rootCmd.AddCommand(getCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// getCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// getCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	getCmd.Flags().StringVarP(&targetF, "dial", "d", "", "target peer to dial")
}

func runClient(ctx context.Context, h host.Host, c cid.Cid, targetPeer string) ([]byte, error) {
	n := bsnet.NewFromIpfsHost(h)
	bswap := bsclient.New(ctx, n, nil, blockstore.NewBlockstore(datastore.NewNullDatastore()))
	n.Start(bswap)
	defer bswap.Close()

	// Turn the targetPeer into a multiaddr.
	maddr, err := multiaddr.NewMultiaddr(targetPeer)
	if err != nil {
		return nil, err
	}

	// Extract the peer ID from the multiaddr.
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}

	// Directly connect to the peer that we know has the content
	// Generally this peer will come from whatever content routing system is provided, however go-bitswap will also
	// ask peers it is connected to for content so this will work
	if err := h.Connect(ctx, *info); err != nil {
		return nil, err
	}

	dserv := merkledag.NewReadOnlyDagService(merkledag.NewSession(ctx, merkledag.NewDAGService(blockservice.New(blockstore.NewBlockstore(datastore.NewNullDatastore()), bswap))))
	nd, err := dserv.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	unixFSNode, err := unixfile.NewUnixfsFile(ctx, dserv, nd)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if f, ok := unixFSNode.(files.File); ok {
		if _, err := io.Copy(&buf, f); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}
