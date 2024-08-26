credits to @tomnomnom
#https://github.com/tomnomnom/fff

Safe URL Fetcher for Bug Bounty Hunting

Options:
  -b, --body <data>         Request body
  -d, --delay <delay>       Delay between issuing requests (ms)
  -H, --header <header>     Add a header to the request (can be specified multiple times)
      --ignore-html         Don't save HTML files; useful when looking for non-HTML files only
      --ignore-empty        Don't save empty files
  -k, --keep-alive          Use HTTP Keep-Alive
  -m, --method              HTTP method to use (default: GET, or POST if body is specified)
  -M, --match <string>      Save responses that include <string> in the body
  -o, --output <dir>        Directory to save responses in (will be created)
  -s, --save-status <code>  Save responses with given status code (can be specified multiple times)
  -S, --save                Save all responses
  -x, --proxy <proxyURL>    Use the provided HTTP proxy
