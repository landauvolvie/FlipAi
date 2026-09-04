package main

import "strings"

// Keep the useful stage filter from the previous Activity screen while the new
// agent-first layout is in place. This also keeps older UI regression tests
// meaningful instead of deleting coverage just because the page was redesigned.
func init() {
	body := activityRedesignHTML
	body = strings.Replace(body,
		`    <select data-activity-range aria-label="Filter by time">`,
		`    <select data-activity-stage aria-label="Filter by stage">
      <option value="">All stages</option>
      <option value="gmail">Gmail</option>
      <option value="routing">Routing</option>
      <option value="agent">Agent</option>
      <option value="reply">Reply</option>
      <option value="security">Security</option>
      <option value="bridge">Bridge</option>
      <option value="host">Host</option>
      <option value="startup">Startup</option>
    </select>
    <select data-activity-range aria-label="Filter by time">`, 1)
	body = strings.Replace(body,
		`.activity2-search-row{display:grid;grid-template-columns:minmax(0,1fr) 145px;gap:10px;margin-bottom:12px}`,
		`.activity2-search-row{display:grid;grid-template-columns:minmax(0,1fr) 135px 145px;gap:10px;margin-bottom:12px}`, 1)
	body = strings.Replace(body,
		`<p class="activity2-privacy">FlipAi logs message flow and status only.`,
		`<p class="activity2-privacy"><b>Privacy:</b> FlipAi logs message flow and status only.`, 1)
	body = strings.Replace(body,
		`var state={events:[],agent:'',query:'',hours:0,page:1,perPage:12};`,
		`var state={events:[],agent:'',stage:'',query:'',hours:0,page:1,perPage:12};`, 1)
	body = strings.Replace(body,
		`      if(state.agent&&eventAgent(e)!==state.agent)return false;
      if(cutoff&&new Date(e.time).getTime()<cutoff)return false;`,
		`      if(state.agent&&eventAgent(e)!==state.agent)return false;
      if(state.stage&&e.stage!==state.stage)return false;
      if(cutoff&&new Date(e.time).getTime()<cutoff)return false;`, 1)
	body = strings.Replace(body,
		`  var range=root.querySelector('[data-activity-range]');if(range)range.addEventListener('change',function(){state.hours=parseFloat(range.value)||0;state.page=1;render();});`,
		`  var stage=root.querySelector('[data-activity-stage]');if(stage)stage.addEventListener('change',function(){state.stage=stage.value||'';state.page=1;render();});
  var range=root.querySelector('[data-activity-range]');if(range)range.addEventListener('change',function(){state.hours=parseFloat(range.value)||0;state.page=1;render();});`, 1)
	registerPage("activity", body)
}
