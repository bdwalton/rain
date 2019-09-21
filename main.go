package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/boltbrowser/boltbrowser"
	clog "github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/console"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/torrent"
	"github.com/hokaccha/go-prettyjson"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli"
	"github.com/zeebo/bencode"
	"gopkg.in/yaml.v2"
)

var (
	app = cli.NewApp()
	clt *rainrpc.Client
	log = logger.New("rain")
)

func main() {
	app.Version = torrent.Version
	app.Usage = "BitTorrent client"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug,d",
			Usage: "enable debug log",
		},
		cli.StringFlag{
			Name:   "cpuprofile",
			Hidden: true,
			Usage:  "write cpu profile to `FILE`",
		},
		cli.StringFlag{
			Name:   "memprofile",
			Hidden: true,
			Usage:  "write memory profile to `FILE`",
		},
		cli.IntFlag{
			Name:   "blockprofile",
			Hidden: true,
			Usage:  "enable blocking profiler",
		},
		cli.StringFlag{
			Name:   "pprof",
			Hidden: true,
			Usage:  "run pprof server on `ADDR`",
		},
	}
	app.Before = handleBeforeCommand
	app.After = handleAfterCommand
	app.Commands = []cli.Command{
		{
			Name:  "server",
			Usage: "run rpc server and torrent client",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config,c",
					Usage: "read config from `FILE`",
					Value: "~/rain/config.yaml",
				},
			},
			Action: handleServer,
		},
		{
			Name:  "client",
			Usage: "send rpc request to server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "url",
					Usage: "URL of RPC server",
					Value: "http://127.0.0.1:" + strconv.Itoa(torrent.DefaultConfig.RPCPort),
				},
			},
			Before: handleBeforeClient,
			Subcommands: []cli.Command{
				{
					Name:   "version",
					Usage:  "server version",
					Action: handleVersion,
				},
				{
					Name:   "list",
					Usage:  "list torrents",
					Action: handleList,
				},
				{
					Name:   "add",
					Usage:  "add torrent or magnet",
					Action: handleAdd,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "torrent,t",
							Usage:    "file or URI",
							Required: true,
						},
						cli.BoolFlag{
							Name:  "stopped",
							Usage: "do not start torrent automatically",
						},
						cli.StringFlag{
							Name:  "id",
							Usage: "if id is not given, a unique id is automatically generated",
						},
					},
				},
				{
					Name:   "remove",
					Usage:  "remove torrent",
					Action: handleRemove,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "stats",
					Usage:  "get stats of torrent",
					Action: handleStats,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "session-stats",
					Usage:  "get stats of session",
					Action: handleSessionStats,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.BoolFlag{
							Name:  "json",
							Usage: "print raw stats as JSON",
						},
					},
				},
				{
					Name:   "trackers",
					Usage:  "get trackers of torrent",
					Action: handleTrackers,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "webseeds",
					Usage:  "get webseed sources of torrent",
					Action: handleWebseeds,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "peers",
					Usage:  "get peers of torrent",
					Action: handlePeers,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "add-peer",
					Usage:  "add peer to torrent",
					Action: handleAddPeer,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "addr",
							Usage:    "peer address in host:port format",
							Required: true,
						},
					},
				},
				{
					Name:   "add-tracker",
					Usage:  "add tracker to torrent",
					Action: handleAddTracker,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "tracker,t",
							Required: true,
							Usage:    "tracker URL",
						},
					},
				},
				{
					Name:   "start",
					Usage:  "start torrent",
					Action: handleStart,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "stop",
					Usage:  "stop torrent",
					Action: handleStop,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "start-all",
					Usage:  "start all torrents",
					Action: handleStartAll,
				},
				{
					Name:   "stop-all",
					Usage:  "stop all torrents",
					Action: handleStopAll,
				},
				{
					Name:   "move",
					Usage:  "move torrent to another server",
					Action: handleMove,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "target",
							Required: true,
							Usage:    "target server in host:port format",
						},
					},
				},
				{
					Name:   "torrent",
					Usage:  "save torrent file",
					Action: handleSaveTorrent,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "out,o",
							Required: true,
						},
					},
				},
				{
					Name:   "magnet",
					Usage:  "get magnet link",
					Action: handleGetMagnet,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:   "console",
					Usage:  "show client console",
					Action: handleConsole,
				},
			},
		},
		{
			Name:   "boltbrowser",
			Hidden: true,
			Action: handleBoltBrowser,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:     "file,f",
					Required: true,
				},
			},
		},
		{
			Name:  "torrent",
			Usage: "manage torrent files",
			Subcommands: []cli.Command{
				{
					Name:   "show",
					Usage:  "show contents of the torrent file",
					Action: handleTorrentShow,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "file,f",
							Required: true,
						},
					},
				},
				{
					Name:   "create",
					Usage:  "create new torrent file",
					Action: handleTorrentCreate,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "file,f",
							Usage:    "include this file or directory in torrent",
							Required: true,
						},
						cli.StringFlag{
							Name:     "out,o",
							Usage:    "save generated torrent to this `FILE`",
							Required: true,
						},
						cli.BoolFlag{
							Name:  "private,p",
							Usage: "create torrent for private trackers",
						},
						cli.IntFlag{
							Name:  "piece-length,l",
							Usage: "override default piece length. by default, piece length calculated automatically based on the total size of files. given in KB. must be multiple of 16.",
						},
						cli.StringFlag{
							Name:  "comment,c",
							Usage: "add `COMMENT` to torrent",
						},
						cli.StringSliceFlag{
							Name:  "tracker,t",
							Usage: "add tracker `URL`",
						},
						cli.StringSliceFlag{
							Name:  "webseed,w",
							Usage: "add webseed `URL`",
						},
					},
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func handleBoltBrowser(c *cli.Context) error {
	db, err := bolt.Open(c.String("file"), 0600, nil)
	if err != nil {
		return err
	}
	boltbrowser.Browse(db, false)
	return db.Close()
}

func handleBeforeCommand(c *cli.Context) error {
	cpuprofile := c.GlobalString("cpuprofile")
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
	}
	pprofFlag := c.GlobalString("pprof")
	if pprofFlag != "" {
		go func() {
			log.Notice(http.ListenAndServe(pprofFlag, nil))
		}()
	}
	blockProfile := c.GlobalInt("blockprofile")
	if blockProfile != 0 {
		runtime.SetBlockProfileRate(blockProfile)
	}
	if c.GlobalBool("debug") {
		logger.SetLevel(clog.DEBUG)
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		pprof.StopCPUProfile()
	}
	return nil
}

func handleServer(c *cli.Context) error {
	cfg := torrent.DefaultConfig

	configPath := c.String("config")
	if configPath != "" {
		cp, err := homedir.Expand(configPath)
		if err != nil {
			return err
		}
		b, err := ioutil.ReadFile(cp) // nolint: gosec
		switch {
		case os.IsNotExist(err):
			log.Noticef("config file not found at %q, using default config", cp)
		case err != nil:
			return err
		default:
			err = yaml.Unmarshal(b, &cfg)
			if err != nil {
				return err
			}
			log.Infoln("config loaded from:", cp)
			b, err = yaml.Marshal(&cfg)
			if err != nil {
				return err
			}
			log.Debug("\n" + string(b))
		}
	}

	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	s := <-ch
	log.Noticef("received %s, stopping server", s)

	memprofile := c.GlobalString("memprofile")
	if memprofile != "" {
		f, err := os.Create(memprofile)
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.WriteHeapProfile(f)
		if err != nil {
			log.Fatal(err)
		}
		err = f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	return ses.Close()
}

func handleBeforeClient(c *cli.Context) error {
	clt = rainrpc.NewClient(c.String("url"))
	return nil
}

func handleVersion(c *cli.Context) error {
	version, err := clt.ServerVersion()
	if err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString(version)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleList(c *cli.Context) error {
	resp, err := clt.ListTorrents()
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleAdd(c *cli.Context) error {
	var b []byte
	var marshalErr error
	arg := c.String("torrent")
	addOpt := &rainrpc.AddTorrentOptions{
		Stopped: c.Bool("stopped"),
		ID:      c.String("id"),
	}
	if strings.HasPrefix(arg, "magnet:") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		resp, err := clt.AddURI(arg, addOpt)
		if err != nil {
			return err
		}
		b, marshalErr = prettyjson.Marshal(resp)
	} else {
		f, err := os.Open(arg) // nolint: gosec
		if err != nil {
			return err
		}
		resp, err := clt.AddTorrent(f, addOpt)
		_ = f.Close()
		if err != nil {
			return err
		}
		b, marshalErr = prettyjson.Marshal(resp)
	}
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleRemove(c *cli.Context) error {
	return clt.RemoveTorrent(c.String("id"))
}

func handleStats(c *cli.Context) error {
	resp, err := clt.GetTorrentStats(c.String("id"))
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleSessionStats(c *cli.Context) error {
	s, err := clt.GetSessionStats()
	if err != nil {
		return err
	}
	if c.Bool("json") {
		b, err := prettyjson.Marshal(s)
		if err != nil {
			return err
		}
		_, _ = os.Stdout.Write(b)
		_, _ = os.Stdout.WriteString("\n")
		return nil
	}
	fmt.Printf("Torrents: %d, Peers: %d, Uptime: %s\n", s.Torrents, s.Peers, time.Duration(s.Uptime)*time.Second)
	fmt.Printf("BlocklistRules: %d, Updated: %s ago\n", s.BlockListRules, time.Duration(s.BlockListRecency)*time.Second)
	fmt.Printf("Reads: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.ReadsPerSecond, s.SpeedRead/1024, s.ReadsActive, s.ReadsPending)
	fmt.Printf("Writes: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.WritesPerSecond, s.SpeedWrite/1024, s.WritesActive, s.WritesPending)
	fmt.Printf("ReadCache Objects: %d, Size: %dMB, Utilization: %d%%\n", s.ReadCacheObjects, s.ReadCacheSize/(1<<20), s.ReadCacheUtilization)
	fmt.Printf("WriteCache Objects: %d, Size: %dMB, PendingKeys: %d\n", s.WriteCacheObjects, s.WriteCacheSize/(1<<20), s.WriteCachePendingKeys)
	fmt.Printf("DownloadSpeed: %dKB/s, UploadSpeed: %dKB/s\n", s.SpeedDownload/1024, s.SpeedUpload/1024)
	return nil
}

func handleTrackers(c *cli.Context) error {
	resp, err := clt.GetTorrentTrackers(c.String("id"))
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleWebseeds(c *cli.Context) error {
	resp, err := clt.GetTorrentWebseeds(c.String("id"))
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handlePeers(c *cli.Context) error {
	resp, err := clt.GetTorrentPeers(c.String("id"))
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleAddPeer(c *cli.Context) error {
	return clt.AddPeer(c.String("id"), c.String("addr"))
}

func handleAddTracker(c *cli.Context) error {
	return clt.AddTracker(c.String("id"), c.String("tracker"))
}

func handleStart(c *cli.Context) error {
	return clt.StartTorrent(c.String("id"))
}

func handleStop(c *cli.Context) error {
	return clt.StopTorrent(c.String("id"))
}

func handleStartAll(c *cli.Context) error {
	return clt.StartAllTorrents()
}

func handleStopAll(c *cli.Context) error {
	return clt.StopAllTorrents()
}

func handleMove(c *cli.Context) error {
	return clt.MoveTorrent(c.String("id"), c.String("target"))
}

func handleConsole(c *cli.Context) error {
	con := console.New(clt)
	return con.Run()
}

func handleTorrentShow(c *cli.Context) error {
	f, err := os.Open(c.String("file")) // nolint: gosec
	if err != nil {
		return err
	}
	defer f.Close()

	val := make(map[string]interface{})
	err = bencode.NewDecoder(f).Decode(&val)
	if err != nil {
		return err
	}
	if info, ok := val["info"].(map[string]interface{}); ok {
		if pieces, ok := info["pieces"].(string); ok {
			info["pieces"] = fmt.Sprintf("<<< %d bytes of data >>>", len(pieces))
		}
	}
	b, err := prettyjson.Marshal(val)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleTorrentCreate(c *cli.Context) error {
	path := c.String("file")
	out := c.String("out")
	private := c.Bool("private")
	pieceLength := c.Uint("piece-length")
	comment := c.String("comment")
	trackers := c.StringSlice("tracker")
	webseeds := c.StringSlice("webseed")

	var err error
	out, err = homedir.Expand(out)
	if err != nil {
		return err
	}
	path, err = homedir.Expand(path)
	if err != nil {
		return err
	}

	tiers := make([][]string, len(trackers))
	for i, tr := range trackers {
		tiers[i] = []string{tr}
	}

	info, err := metainfo.NewInfoBytes(path, private, uint32(pieceLength<<10))
	if err != nil {
		return err
	}
	mi, err := metainfo.NewBytes(info, tiers, webseeds, comment)
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	_, err = f.Write(mi)
	if err != nil {
		return err
	}
	return f.Close()
}

func handleSaveTorrent(c *cli.Context) error {
	torrent, err := clt.GetTorrent(c.String("id"))
	if err != nil {
		return err
	}
	f, err := os.Create(c.String("out"))
	if err != nil {
		return err
	}
	_, err = f.Write(torrent)
	if err != nil {
		return err
	}
	return f.Close()
}

func handleGetMagnet(c *cli.Context) error {
	magnet, err := clt.GetMagnet(c.String("id"))
	if err != nil {
		return err
	}
	fmt.Println(magnet)
	return nil
}
