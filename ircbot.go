package main

import (
    "code.google.com/p/go.net/html"
    "encoding/json"
    "github.com/thoj/go-ircevent"
    "io"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "regexp"
    "strings"
)

type Configuration struct {
    Server      string
    SSL         bool
    Nick        string
    Username    string
    RoomName    string
    IgnoreRegex string
    Debug       bool
}

type URLHistory struct {
    Id   int
    URL  string
    Date string
    Who  string
}

// Set up Logger
var l = log.New(os.Stdout, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)

func main() {
    // Load Configuration
    file, _ := os.Open("conf.json")
    decoder := json.NewDecoder(file)
    conf := Configuration{}
    err := decoder.Decode(&conf)
    if err != nil {
        l.Print("error:", err)
    }

    l.Print("Starting up.")

    con := irc.IRC(conf.Nick, conf.Username)
    con.Debug = conf.Debug
    con.UseTLS = conf.SSL
    err = con.Connect(conf.Server)

    if err != nil {
        l.Print("Failed connection")
        return
    }

    l.Print("Connected.", conf.Server)
    // When we've connected to the IRC server, go join the room!
    con.AddCallback("001", func(e *irc.Event) {
        con.Join(conf.RoomName)
    })

    /* Say something on arrival
       con.AddCallback("JOIN", func(e *irc.Event) {
           con.Privmsg(conf.RoomName, "Hello!")
       }) */

    // Check each message to see if it contains a URL, and return the title
    con.AddCallback("PRIVMSG", func(e *irc.Event) {
        // Regex to catch web URLs.
        var webaddress = regexp.MustCompile("http(s)?\\S*")
        var ignoreRegex = regexp.MustCompile(conf.IgnoreRegex)

        ignoreIt := ignoreRegex.FindString(strings.ToLower(e.Message()))

        if ignoreIt == "" {
            matched := webaddress.FindString(e.Message())

            if matched != "" {
                // We found a URL.  Fetch the page
                resp, err := http.Get(matched)
                if err != nil {
                    // Problem getting the page
                    l.Print("%s", err)
                } else {
                    // Wait until finished getting the content
                    defer resp.Body.Close()
                    contents, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<18))
                    //contents := io.LimitReader(resp.Body, 2048)
                    if err != nil {
                        // Had a problem fetching the page
                        l.Printf("%s", err)
                    } else if resp.StatusCode != 200 {
                        l.Printf("%s returned %s", matched, resp.Status)
                    } else {
                        // Find the title from the page, log and send it to IRC channel
                        title := getTitle(string(contents))
                        l.Print("Went to ", matched, " got title ", title)
                        // Maybe look into how I can make this bot use more than one room?
                        con.Privmsg(conf.RoomName, title)
                    }
                }
            }
        } else {
            l.Print("Message is from ignored user, ", ignoreIt)
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
