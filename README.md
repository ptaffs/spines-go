# spines-go
A version of spines in GO language which reduces dependencies on all sorts of things, but mainly Apache httpd and then installing the correct files in the correct folders, since Golang has API handlers and asset serving.

It also scans the library on start, taking just a few seconds and keeping the database in memory rather than accessing the text database on every action, and solving the unsolved need to escape text delimeter (a pipe) in the CSV database, allowing the inclusion of M|A|R|R|S finally!

It still relies on music player daemon (https://www.musicpd.org/) to drive audio output, and the principle that albums are defined by the playlist file called "album.m3u" or cue in the music directory.

Spines 1 defined studio albums, classical music, soundtracks by different playlist names, which were presented sequentially in the shelf, with soundtrack albums last, Spines-Go uses the first folder in the path <Music>/Albums/ <Music>/Classical/ etc which will allow flexibility to the user and simplicty in the playlist finding code. My music library was already organised this way.
