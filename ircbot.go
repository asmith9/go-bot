package main

import (
    "code.google.com/p/go.net/html"
    "database/sql"
    "encoding/json"
    log "github.com/cihub/seelog"
    "github.com/coopernurse/gorp"
    _ "github.com/mattn/go-sqlite3"
    "github.com/thoj/go-ircevent"
    "io"
    "io/ioutil"
    "net/http"
    "os"
    "regexp"
    "strings"
    "time"
)

type Configuration struct {
    Server       string
    SSL          bool
    Nick         string
    Username     string
    RoomName     string
    IgnoreRegex  string
    Debug        bool
    HelloMessage string
    Driver       string
    Database     string
}

func init() {
    // Set up the logger
    logger, err := log.LoggerFromConfigAsFile("logging.xml")

    if err != nil {
        log.Debug(err)
        return
    }

    log.ReplaceLogger(logger)
    return
}

func main() {
    defer log.Flush()
    // Load Configuration
    file, _ := os.Open("conf.json")
    decoder := json.NewDecoder(file)
    conf := Configuration{}
    err := decoder.Decode(&conf)
    checkErr(err, "Failed to read configuarion")

    log.Info("Starting up, connecting to ", conf.Server, " as ", conf.Nick)

    // Connect to database
    db := initDb(conf)
    defer db.Db.Close()

    // Connect to IRC server
    con := irc.IRC(conf.Nick, conf.Username)
    con.Debug = conf.Debug
    con.UseTLS = conf.SSL
    err = con.Connect(conf.Server)
    checkErr(err, "Failed to connect to IRC server")
    defer con.Disconnect()

    log.Info("Connected: ", conf.Server)
    // When we've connected to the IRC server, go join the room!
    con.AddCallback("001", func(e *irc.Event) {
        con.Join(conf.RoomName)
        log.Info("Joined room ", conf.RoomName)
    })

    if conf.HelloMessage != "" {
        // Say something on arrival
        con.AddCallback("JOIN", func(e *irc.Event) {
            con.Privmsg(conf.RoomName, conf.HelloMessage)
        })
    }
    // Check each message to see if it contains a URL, and return the title
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        // Regex to catch web URLs.
        var webaddress = regexp.MustCompile("http(s)?\\S*")
        var ignoreRegex = regexp.MustCompile(conf.IgnoreRegex)

        ignoreIt := ignoreRegex.FindString(strings.ToLower(e.Nick))

        if ignoreIt == "" {
            matched := webaddress.FindString(e.Message())

            if matched != "" {
                // We found a URL.  Fetch the page
                resp, err := http.Get(matched)
                if err != nil {
                    log.Debug(err)
                } else {
                    // Wait until finished getting the content
                    defer resp.Body.Close()
                    contents, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<18))
                    if err != nil {
                        log.Debug(err)
                    } else if resp.StatusCode != 200 {
                        log.Debug(matched, "returned with status code:", resp.Status)
                    } else {
                        // Find the title from the page, log and send it to IRC channel
                        title := getTitle(string(contents))
                        log.Debug("Went to ", matched, " got title ", title)
                        con.Privmsg(conf.RoomName, title)
                        log.Debug("Adding entry to database")
                        url := newURL(matched, e.Nick)
                        err = db.Insert(&url)
                        checkErr(err, "Insert Failed!")
                    }
                }
            }
        } else {
            log.Info("Message is from ignored user, ", ignoreIt)
        }
    })
    // Let's just keep on keeping on
    con.Loop()
}

func getTitle(body string) string {
    // Setting a default title
    title := "No Title Found"
    // Splits the html up into a series of tokens (tags)
    d := html.NewTokenizer(strings.NewReader(body))
    for {
        tokenType := d.Next()
        if tokenType == html.ErrorToken {
            return title
        }
        token := d.Token()
        if tokenType == html.StartTagToken {
            if strings.ToLower(token.Data) == "title" {
                tokenType := d.Next()
                if tokenType == html.TextToken {
                    return d.Token().Data
                }
            }
        }
    }
    return title
}

type URL struct {
    Id   int
    URL  string
    Date int64
    Who  string
}

func initDb(conf Configuration) *gorp.DbMap {
    // connect to db using standard Go database/sql API
    // use whatever database/sql driver you wish
    db, err := sql.Open(conf.Driver, conf.Database)
    checkErr(err, "sql.Open failed")

    // construct a gorp DbMap
    dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}

    // add a table, setting the table name to 'posts' and
    // specifying that the Id property is an auto incrementing PK
    dbmap.AddTableWithName(URL{}, "urls").SetKeys(true, "Id")

    // create the table. in a production system you'd generally
    // use a migration tool, or create the tables via scripts
    err = dbmap.CreateTablesIfNotExists()
    checkErr(err, "Create tables failed")

    return dbmap
}

func newURL(url, who string) URL {
    return URL{
        Date: time.Now().UnixNano(),
        URL:  url,
        Who:  who,
    }
}

func checkErr(err error, msg string) {
    if err != nil {
        log.Critical(msg, err)
    }
}
