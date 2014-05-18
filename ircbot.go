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

type URL struct {
    URL   string
    Date  int64
    Who   string
    Title string
}

type Seen struct {
    Who     string
    Date    int64
    Message string
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
    file, err := os.Open("conf.json")
    checkErr(err, "Failed to read configuarion")
    defer file.Close()
    decoder := json.NewDecoder(file)
    conf := Configuration{}
    err = decoder.Decode(&conf)
    checkErr(err, "Failed to read configuarion")

    // Connect to database
    log.Debug("Connecting to Database")
    db := initDb(conf)
    defer db.Db.Close()

    log.Debug("Starting up, connecting to ", conf.Server, " as ", conf.Nick)
    // Connect to IRC server
    con := irc.IRC(conf.Nick, conf.Username)
    con.Debug = conf.Debug
    con.UseTLS = conf.SSL
    err = con.Connect(conf.Server)
    checkErr(err, "Failed to connect to IRC server")
    defer con.Disconnect()

    // When we've connected to the IRC server, go join the room!
    log.Info("Connected: ", conf.Server)
    con.AddCallback("001", func(e *irc.Event) {
        con.Join(conf.RoomName)
        log.Info("Joined room ", conf.RoomName)
    })

    // Say something on arrival
    if conf.HelloMessage != "" {
        con.AddCallback("JOIN", func(e *irc.Event) {
            con.Privmsg(conf.RoomName, conf.HelloMessage)
        })
    }
    // Check each message to see if it contains a URL, and return the title
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        go urlHandler(conf, e, con, db)
    })
    // Keep updated on last time an individual was seen
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        go seenHandler(e, con, db)
    })
    // Keep updated on last time an individual was seen
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        go seenRequest(conf, e, con, db)
    })
    // Let's just keep on keeping on
    con.Loop()
}

func seenHandler(e *irc.Event, con *irc.Connection, db *gorp.DbMap) {
    /* This function is pretty lightweight but does involve
       a db write per message received.  As I have the db layer there
       i'm going to use it for now, but it might be worth just doing this in-memory.
       In memory shouldn't get too large, unless the bot is coming in to contact
       with an extremely large number of users.  It does mean it would forget everything after
       each restart, unless I came up with a background process to flush to disk periodically. */
    obj, err := db.Get(Seen{}, e.Nick)
    checkErr(err, "Error requesting user from database")
    if obj != nil {
        log.Debug("User found in db, updating entry")
        seen := newSeen(e.Nick, e.Message())
        _, err := db.Update(&seen)
        checkErr(err, "Update Failed!")
    } else {
        log.Debug("User wasn't found in db, adding new record")
        seen := newSeen(e.Nick, e.Message())
        err := db.Insert(&seen)
        checkErr(err, "Update Failed!")
    }
    return
}

func seenRequest(conf Configuration, e *irc.Event, con *irc.Connection, db *gorp.DbMap) {
    /* Return details of the last time a nick was seen in channel, if possible */

    // We're only interested in messages starting with #seen
    if strings.HasPrefix(strings.ToLower(e.Message()), "#seen") {
        log.Debug("Message started with #seen")
        users := strings.SplitAfter(e.Message(), "#seen ")
        for i := range users {
            if i == 0 {
                continue
            }
            obj, err := db.Get(Seen{}, users[i])
            checkErr(err, "Error requesting user from database")
            if obj != nil {
                log.Debug("Found user ", users[i], " in the database")
                seen := obj.(*Seen)
                lastSeen := time.Duration(time.Now().Unix()-seen.Date) * time.Second
                message := e.Nick + ": " + users[i] + " was last seen " + lastSeen.String() + " ago."
                con.Privmsg(conf.RoomName, message)
            } else {
                log.Debug("Didn't find user ", users[i], " in the database")
                message := e.Nick + ": Sorry, I have never seen " + users[i] + " before"
                con.Privmsg(conf.RoomName, message)
            }
        }
    }
    return
}

func urlHandler(conf Configuration, e *irc.Event, con *irc.Connection, db *gorp.DbMap) {
    // Regex to catch web URLs.
    var webaddress = regexp.MustCompile("http(s)?\\S*")
    var ignoreRegex = regexp.MustCompile(conf.IgnoreRegex)

    ignoreIt := ignoreRegex.FindString(strings.ToLower(e.Nick))

    if ignoreIt == "" {
        matched := webaddress.FindString(e.Message())

        if matched != "" {
            // We found a URL, first check if it already exists
            obj, err := db.Get(URL{}, matched)
            checkErr(err, "Error requesting URL from database")
            if obj != nil {
                log.Debug("Found URL in the database")
                url := obj.(*URL)
                if (time.Now().Unix() - url.Date) < 86400 { // 86400 seconds = 1 day
                    log.Debug("Title in database is fresh enough")
                    message := url.Title + ".  Originally posted by: " + url.Who
                    con.Privmsg(conf.RoomName, message)
                    return
                }
                log.Debug("Title in database isn't fresh enough")
            }
            // We need to fetch an up to date title
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
                    if obj != nil {
                        log.Debug("Updating entry in database")
                        url := newURL(matched, e.Nick, title)
                        _, err := db.Update(&url)
                        checkErr(err, "Update Failed!")
                    } else {
                        log.Debug("Adding entry to database")
                        url := newURL(matched, e.Nick, title)
                        if obj != nil {
                            err = db.Insert(&url)
                        } else {
                            err = db.Insert(&url)
                        }

                        checkErr(err, "Insert Failed!")
                    }
                }
            }
        }
    } else {
        log.Info("Message is from ignored user, ", ignoreIt)
    }
    return
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

func initDb(conf Configuration) *gorp.DbMap {
    // connect to db using standard Go database/sql API
    // use whatever database/sql driver you wish
    db, err := sql.Open(conf.Driver, conf.Database)
    checkErr(err, "sql.Open failed")

    // construct a gorp DbMap
    dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}

    // add a table, setting the table name to 'posts' and
    // specifying that the Id property is an auto incrementing PK
    dbmap.AddTableWithName(URL{}, "urls").SetKeys(false, "URL")
    dbmap.AddTableWithName(Seen{}, "seen").SetKeys(false, "Who")

    // create the table. in a production system you'd generally
    // use a migration tool, or create the tables via scripts
    err = dbmap.CreateTablesIfNotExists()
    checkErr(err, "Create tables failed")

    return dbmap
}

func newURL(url, who, title string) URL {
    return URL{
        Date:  time.Now().Unix(),
        URL:   url,
        Who:   who,
        Title: title,
    }
}

func newSeen(who, message string) Seen {
    return Seen{
        Date:    time.Now().Unix(),
        Who:     who,
        Message: message,
    }
}

func checkErr(err error, msg string) {
    if err != nil {
        log.Critical(msg, " ", err)
    }
}
