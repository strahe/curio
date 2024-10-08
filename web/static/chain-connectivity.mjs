import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

window.customElements.define('chain-connectivity', class MyElement extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        const blockDelay = await RPCCall('BlockDelaySecs')
        await this.updateData();
        setTimeout(()=>this.loadData(), blockDelay * 1000);
    };

    async updateData() {
        this.data = await RPCCall('SyncerState');
        this.requestUpdate();
    }

    static get styles() {
        return [css`
        :host {
            box-sizing: border-box; /* Don't forgert this to include padding/border inside width calculation */
        }
    `];
    }
    render() {
        return html`
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
    <link rel="stylesheet" href="/ux/main.css">
  <link href="https://fonts.cdnfonts.com/css/metropolis-2" rel="stylesheet" crossorigin="anonymous">
  <table class="table table-dark">
    <thead>
        <tr>
            <th>RPC Address</th>
            <th>Reachability</th>
            <th>Sync Status</th>
            <th>Version</th>
        </tr>
    </thead>
    <tbody>
        ${this.data.map(item => html`
        <tr>
            <td>${item.Address}</td>
            <td>${item.Reachable ? html`<span class="success">ok</span>` : html`<span class="error">FAIL</span>`}</td>
            <td>${item.SyncState === "ok" ? html`<span class="success">ok</span>` : html`<span class="warning">No${item.SyncState? ', '+item.SyncState:''}</span>`}</td>
            <td>${item.Version}</td>
        </tr>
        `)}
    </tbody>
  </table>`
    }
});
