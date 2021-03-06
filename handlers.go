package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func uploadHandler(store Store, secret string) http.HandlerFunc {
	const oneMB = 1e6
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(oneMB); err != nil {
			httpFailf(w, http.StatusBadRequest, "parse: %s", err)
			return
		}

		fd, _, err := r.FormFile("content")
		defer fd.Close()
		content, err := ioutil.ReadAll(fd)
		if err != nil {
			httpFailf(w, http.StatusBadRequest, "read content: %s", err)
			return
		}
		content = bytes.TrimSpace(content)
		if len(content) == 0 {
			httpFailf(w, http.StatusBadRequest, "content is required")
			return
		}

		commit := strings.TrimSpace(r.Form.Get("commit"))
		if commit == "" {
			httpFailf(w, http.StatusBadRequest, "commit is required")
			return
		}

		if sig := w.Header().Get("signature"); !signed(sig, content, secret) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if len(content) < 10 {
			// Ignore dummy content.
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		benchID, err := store.CreateBenchmark(r.Context(), string(content), commit)
		if err != nil {
			httpFailf(w, http.StatusInternalServerError, "cannot upload: %s", err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, benchID)
	}
}

func signed(sig string, content []byte, secret string) bool {

	// TODO: check the signature of the content to make sure the signer
	// knows the secret.

	return true
}

func showBenchmark(store Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		benchID, _ := strconv.ParseInt(lastChunk(r.URL.Path), 10, 64)
		if benchID == 0 {
			httpFailf(w, http.StatusNotFound, "benchmark not found")
			return
		}

		switch bench, err := store.FindBenchmark(r.Context(), benchID); err {
		case nil:
			io.WriteString(w, bench.Content)
		case ErrNotFound:
			httpFailf(w, http.StatusNotFound, "benchmark not found")
		default:
			httpFailf(w, http.StatusInternalServerError, err.Error())
		}
	}
}

func lastChunk(path string) string {
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func listHandler(store Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		benchmarks, err := store.ListBenchmarks(r.Context(), time.Now(), 100)
		if err != nil {
			httpFailf(w, http.StatusInternalServerError, "cannot list benchmarks: %s", err)
			return
		}
		tmpl.ExecuteTemplate(w, "listing", benchmarks)
	}
}

func compareHandler(store Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		query := r.URL.Query()

		if query.Get("a") == "" || query.Get("b") == "" {
			httpFailf(w, http.StatusBadRequest, "Missing benchmarks IDs. Usage %s?a=<ID>&b=<ID>", r.URL.Path)
			return
		}

		aID, _ := strconv.ParseInt(query.Get("a"), 10, 64)
		a, err := store.FindBenchmark(ctx, aID)
		if err != nil {
			code := http.StatusInternalServerError
			if err == ErrNotFound {
				code = http.StatusNotFound
			}
			httpFailf(w, code, "cannot find benchmark %d: %s", aID, err)
			return
		}

		bID, _ := strconv.ParseInt(query.Get("b"), 10, 64)
		b, err := store.FindBenchmark(ctx, bID)
		if err != nil {
			code := http.StatusInternalServerError
			if err == ErrNotFound {
				code = http.StatusNotFound
			}
			httpFailf(w, code, "cannot find benchmark %d: %s", bID, err)
			return
		}

		cmp, err := Compare(a, b)
		if err != nil {
			httpFailf(w, http.StatusInternalServerError, "cannot compare: %s", err)
			return
		}

		w.Write(cmp)
	}
}

func httpFailf(w http.ResponseWriter, code int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	w.Header().Set("content-type", "text/html;charset=utf-8")
	w.WriteHeader(code)
	tmpl.ExecuteTemplate(w, "error", msg)
}

var tmpl = template.Must(template.New("").Parse(`

{{define "header"}}
<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
	*              { box-sizing: border-box; }
	html           { position: relative; min-height: 100%; margin: 20px; width: 100%; max-width: 800px; margin: 20px auto; }
	body           { margin: 40px auto 120px auto; max-width: 50em; line-height: 28px; }
	.error         { background: #fce8f3; border: 1px solid #fcbfe2; padding: 10px 20px; border-radius: 3px; }
	table          { min-width: 80%; }
	table thead    { font-weight: bolder; }
	table tbody td { border-top: 1px solid #e0e0e0; padding: 3px 5px; }
	button         { margin-top: 30px; }
</style>
{{ end}}

{{define "error"}}
	{{template "header" .}}
	<div class="error">{{.}}</div>
{{end}}

{{define "listing"}}
	{{template "header" .}}
	{{if .}}
		<form action="/compare/" method="GET">
			<table>
			<thead>
				<tr>
					<td>ID</td>
					<td>Compare</td>
					<td>Created</td>
					<td>Commit</td>
				</tr>
			</thead>
			{{range .}}
				<tbody>
					<tr>
						<td>
							<a href="/benchmarks/{{.ID}}">
								#{{.ID}}
							</a>
						</td>
						<td>
							<input type="radio" name="a" value="{{.ID}}">
							<input type="radio" name="b" value="{{.ID}}">
						</td>
						<td>{{.Created.Format "Mon, 2 Jan 15:04"}}</td>
						<td>{{.Commit}}</td>
					</tr>
				</tbody>
			{{end}}
			</table>
			<button type="submit">Compare</button>
		</form>
	{{else}}
		<div class="error">No benchmarks.</div>
	{{end}}
{{end}}
`))
