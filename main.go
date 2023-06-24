package main

import (
	"math/rand"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/cjim8889/dht-crawler/bittorrent/metadata"
	"github.com/cjim8889/dht-crawler/dht"
	"github.com/cjim8889/dht-crawler/persistence"
	"github.com/cjim8889/dht-crawler/util"
	"github.com/pkg/errors"
	"github.com/pkg/profile"

	"github.com/jessevdk/go-flags"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/Wessie/appdirs"
)

type opts struct {
	DatabaseURL         string        `long:"database" env:"DATABASE_URL" description:"URL of the database."`
	IndexerAddrs        []string      `long:"indexer-addr" env:"INDEXER_ADDR" default:"0.0.0.0:0" description:"Address(es) to be used by indexing DHT nodes."`
	IndexerInterval     time.Duration `long:"indexer-interval" env:"INDEXER_INTERVAL" default:"1s" description:"Indexing interval in integer seconds."`
	IndexerMaxNeighbors uint          `long:"indexer-max-neighbors" env:"INDEXER_MAX_NEIGHBORS" default:"1000" description:"Maximum number of neighbors of an indexer."`
	LeechMaxN           uint          `long:"leech-max-n" env:"LEECH_MAX_N" default:"50" description:"Maximum number of leeches."`
	Verbosity           int           `long:"verbose" env:"VERBOSITY" default:"1" description:"Increases verbosity."`
	Profile             string        `long:"profile" env:"PROFILE" description:"Enable profiling."`
}

var compiledOn string

func main() {
	loggerLevel := zap.NewAtomicLevel()
	// Logging levels: ("debug", "info", "warn", "error", "dpanic", "panic", and "fatal").
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stderr),
		loggerLevel,
	))
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	// opFlags is the "operational flags"
	opFlags, err := parseFlags()
	if err != nil {
		// Do not print any error messages as jessevdk/go-flags already did.
		return
	}

	zap.L().Info("dht-crawler v0.0.1 has been started.")
	zap.L().Info("Copyright (C) 2023")
	zap.S().Infof("Compiled on %s", compiledOn)

	switch opFlags.Verbosity {
	case 0:
		loggerLevel.SetLevel(zap.WarnLevel)
	case 1:
		loggerLevel.SetLevel(zap.InfoLevel)
	default: // Default: i.e. in case of 2 or more.
		// TODO: print the caller (function)'s name and line number!
		loggerLevel.SetLevel(zap.DebugLevel)
	}

	zap.ReplaceGlobals(logger)

	switch opFlags.Profile {
	case "cpu":
		defer profile.Start(profile.CPUProfile, profile.ProfilePath("."), profile.NoShutdownHook).Stop()
	case "memory":
		defer profile.Start(
			profile.MemProfile,
			profile.ProfilePath("."),
			profile.NoShutdownHook,
			profile.MemProfileRate(1),
		).Stop()
	}

	// Initialise the random number generator
	rand.Seed(time.Now().UnixNano())

	// Handle Ctrl-C gracefully.
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)

	database, err := persistence.MakeDatabase(opFlags.DatabaseURL, logger)
	if err != nil {
		logger.Fatal("Could not open the database", zap.String("url", opFlags.DatabaseURL), zap.Error(err))
	}

	trawlingManager := dht.NewManager(opFlags.IndexerAddrs, opFlags.IndexerInterval, opFlags.IndexerMaxNeighbors)
	metadataSink := metadata.NewSink(5*time.Second, opFlags.Verbosity)

	// The Event Loop
	for stopped := false; !stopped; {
		select {
		case result := <-trawlingManager.Output():
			infoHash := result.InfoHash()

			zap.L().Debug("Trawled!", util.HexField("infoHash", infoHash[:]))
			exists, err := database.DoesTorrentExist(infoHash[:])
			if err != nil {
				zap.L().Fatal("Could not check whether torrent exists!", zap.Error(err))
			} else if !exists {
				metadataSink.Sink(result)
			}

		case md := <-metadataSink.Drain():
			if err := database.AddNewTorrent(md.InfoHash, md.Name, md.Files); err != nil {
				zap.L().Fatal("Could not add new torrent to the database",
					util.HexField("infohash", md.InfoHash), zap.Error(err))
			}
			zap.L().Info("Fetched!", zap.String("name", md.Name), util.HexField("infoHash", md.InfoHash))

		case <-interruptChan:
			trawlingManager.Terminate()
			stopped = true
		}
	}

	if err = database.Close(); err != nil {
		zap.L().Error("Could not close database!", zap.Error(err))
	}
}

func parseFlags() (*opts, error) {
	o := &opts{}
	_, err := flags.Parse(o)
	if err != nil {
		return nil, err
	}

	if o.DatabaseURL == "" {
		o.DatabaseURL =
			"sqlite3://" +
				appdirs.UserDataDir("magneticod", "", "", false) +
				"/database.sqlite3" +
				"?_journal_mode=WAL" + // https://github.com/mattn/go-sqlite3#connection-string
				"&_busy_timeout=3000" + // in milliseconds
				"&_foreign_keys=true"

	}

	if err = checkAddrs(o.IndexerAddrs); err != nil {
		zap.S().Fatalf("Of argument (list) `trawler-ml-addr`", zap.Error(err))
	}

	if o.LeechMaxN > 1000 {
		zap.S().Warnf(
			"Beware that on many systems max # of file descriptors per process is limited to 1024. " +
				"Setting maximum number of leeches greater than 1k might cause \"too many open files\" errors!",
		)
	}

	return o, nil
}

func checkAddrs(addrs []string) error {
	for i, addr := range addrs {
		// We are using ResolveUDPAddr but it works equally well for checking TCPAddr(esses) as
		// well.
		_, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return errors.Wrapf(err, "%d(th) address (%s) error", i+1, addr)
		}
	}
	return nil
}
