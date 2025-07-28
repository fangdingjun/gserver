amd64:
	CGO_ENABLED=0 go build -ldflags "-s -w"
win:
	GOOS=windows GOARCH=amd64 go build -o gserver.exe -ldflags "-s -w"
pi:
	GOOS=linux GOARCH=arm GOARM=6 go build -o gserver_arm -ldflags "-s -w"
lua:
	CGO_LDFLAGS="-L${HOME}/.local/lib" go build  -o gserver_lua -tags lua54 -ldflags "-s -w"