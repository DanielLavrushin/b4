package mtproto

import (
	"net/http"
	"strings"
)

func webPageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "frame-ancestors 'self' http://127.0.0.1:* http://[::1]:*")
	h.Del("X-Frame-Options")
}

func webWriteSite(w http.ResponseWriter, r *http.Request, status int) {
	webPageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(webSitePage))
}

const webSitePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>Service status</title>
<style>
:root{color-scheme:light dark}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
background:#f6f7f9;color:#23282d}
main{max-width:30rem;padding:2rem}
h1{font-size:1.25rem;margin:0 0 .5rem}
p{margin:.4rem 0;color:#5c6670}
code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.85em}
@media(prefers-color-scheme:dark){body{background:#16191c;color:#e6e8ea}p{color:#9aa4ad}}
</style></head>
<body><main>
<h1>Service status</h1>
<p>This endpoint is operational.</p>
<p>Automated monitoring only. There is nothing to configure here.</p>
</main></body></html>
`

func webBridgePage() []byte {
	return []byte(strings.Replace(webBridgeTemplate, "__CARRIER__", webCarrierPath, 1))
}

const webBridgeTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>Service status</title>
<style>body{margin:0;font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;background:#f6f7f9;color:#23282d}main{max-width:30rem;padding:2rem}@media(prefers-color-scheme:dark){body{background:#16191c;color:#e6e8ea}}</style>
</head>
<body><main><h1 style="font-size:1.25rem;margin:0 0 .5rem">Service status</h1><p id="s" style="color:#5c6670">Connecting…</p></main>
<script>
(function(){
var params=new URLSearchParams(location.search);
var cap=params.get('bridge')||'';
var nonce='';
if(location.hash.indexOf('#android=')===0)nonce=location.hash.slice(9);
try{history.replaceState(null,'','/')}catch(e){}

var ws=null,wsOpen=false,closed=false;
var toWs=[],toClient=[],client=null,status=null,port=null;
var label=document.getElementById('s');
var upBytes=0,downBytes=0,trafficTimer=0;

function reportTraffic(){
 if(!port||(!upBytes&&!downBytes))return;
 var up=upBytes,down=downBytes;upBytes=0;downBytes=0;
 try{port.postMessage({t:'traffic',up:up,down:down})}catch(e){}
}
function flushWs(){while(wsOpen&&toWs.length){var buf=toWs.shift(),n=buf.byteLength;try{ws.send(buf)}catch(e){shutdown();return}upBytes+=n}}
function flushClient(){while(client&&toClient.length){try{client(toClient.shift())}catch(e){shutdown();return}}}
function setStatus(state){
 if(label)label.textContent=state==='connected'?'Operational.':state==='failed'?'Temporarily unavailable.':'Connecting…';
 if(status)try{status(state)}catch(e){}
}
function shutdown(){
 if(closed)return;
 closed=true;
 reportTraffic();
 if(trafficTimer){clearInterval(trafficTimer);trafficTimer=0}
 setStatus('failed');
 if(port)try{port.postMessage({t:'close'})}catch(e){}
 if(ws)try{ws.close()}catch(e){}
}
function connect(){
 var scheme=location.protocol==='https:'?'wss://':'ws://';
 try{ws=new WebSocket(scheme+location.host+'__CARRIER__?b='+encodeURIComponent(cap))}catch(e){shutdown();return}
 ws.binaryType='arraybuffer';
 ws.onopen=function(){wsOpen=true;setStatus('connected');flushWs()};
 ws.onmessage=function(e){if(e.data instanceof ArrayBuffer){downBytes+=e.data.byteLength;toClient.push(e.data);flushClient()}};
 ws.onerror=function(){setStatus('failed')};
 ws.onclose=function(){wsOpen=false;shutdown()};
}
function fromClient(buf){toWs.push(buf);flushWs()}

var bridge=window.TelegramWebProxy;
if(bridge){
 status=function(state){bridge.postMessage(JSON.stringify({t:'status',state:state}))};
 client=function(buf){bridge.postMessage(buf)};
 bridge.onmessage=function(ev){
  var d=ev.data;
  if(typeof d==='string'){
   var control=null;
   try{control=JSON.parse(d)}catch(e){return}
   if(control&&control.t==='close')shutdown();
   return;
  }
  if(d instanceof ArrayBuffer)fromClient(d);
 };
 connect();
 bridge.postMessage(JSON.stringify({t:'tproxy-android-init',v:1,nonce:nonce}));
 setStatus('connecting');
}else{
 addEventListener('message',function(ev){
  if(port)return;
  if(!/^http:\/\/(127\.0\.0\.1|\[::1\]):[0-9]{1,5}$/.test(ev.origin))return;
  var d=ev.data;
  if(!d||d.t!=='tproxy-init'||d.v!==1||!ev.ports||!ev.ports[0])return;
  port=ev.ports[0];
  status=function(state){port.postMessage({t:'status',state:state})};
  client=function(buf){port.postMessage(buf,[buf])};
  port.onmessage=function(e){if(e.data instanceof ArrayBuffer)fromClient(e.data)};
  port.start();
  flushClient();
  setStatus(wsOpen?'connected':'connecting');
  trafficTimer=setInterval(reportTraffic,1000);
 });
 connect();
}
})();
</script></body></html>
`
