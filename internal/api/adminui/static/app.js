function app() {
  return {
    authenticated: false,
    loginKey: '',
    loginError: '',
    loginLoading: false,
    currentView: 'accounts',
    accounts: [],
    detailAccount: null,
    quotas: [],
    health: {},
    actionLoading: false,
    quotaLoading: false,
    quotaRefreshing: {},
    chatTestModel: 'claude-haiku-4.5',
    chatTestMessage: 'Hi',
    chatTestLoading: false,
    chatTestResult: null,
    toasts: [],
    toastId: 0,
    showAddModal: false,
    addLoading: false,
    addForm: { label: '', auth_method: 'social', refresh_token: '', api_key: '', profile_arn: '', region: 'us-east-1', proxy_url: '' },
    showEditModal: false,
    editLoading: false,
    editForm: { id: '', label: '', region: '', proxy_url: '' },
    showDisableModal: false,
    disableLoading: false,
    disableReason: '',
    disableAccountId: '',
    showDeleteModal: false,
    deleteLoading: false,
    deleteTarget: null,
    healthInterval: null,
    quotaInterval: null,

    init() {
      var key = sessionStorage.getItem('kiro_admin_api_key');
      if (key) {
        this.authenticated = true;
        this.loginKey = key;
        this.loadAccounts();
        this.loadQuota();
        this.loadHealth();
        this.startHealthPoll();
        this.startQuotaPoll();
      }
    },

    async login() {
      if (!this.loginKey.trim()) return;
      this.loginLoading = true;
      this.loginError = '';
      try {
        var res = await fetch('/admin/accounts', {
          headers: { 'Authorization': 'Bearer ' + this.loginKey.trim() }
        });
        if (res.status === 401) {
          this.loginError = 'Invalid API key';
          return;
        }
        if (!res.ok) {
          this.loginError = 'Connection failed: ' + res.status;
          return;
        }
        sessionStorage.setItem('kiro_admin_api_key', this.loginKey.trim());
        this.authenticated = true;
        this.loadAccounts();
        this.loadQuota();
        this.loadHealth();
        this.startHealthPoll();
        this.startQuotaPoll();
      } catch (e) {
        this.loginError = 'Network error: ' + e.message;
      } finally {
        this.loginLoading = false;
      }
    },

    logout() {
      sessionStorage.removeItem('kiro_admin_api_key');
      this.authenticated = false;
      this.loginKey = '';
      this.accounts = [];
      this.quotas = [];
      this.detailAccount = null;
      if (this.healthInterval) clearInterval(this.healthInterval);
      if (this.quotaInterval) clearInterval(this.quotaInterval);
      this.health = {};
      if (this.healthInterval) {
        clearInterval(this.healthInterval);
        this.healthInterval = null;
      }
    },

    async apiCall(method, path, body) {
      var key = sessionStorage.getItem('kiro_admin_api_key');
      var opts = {
        method: method,
        headers: { 'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json' }
      };
      if (body) opts.body = JSON.stringify(body);
      var res = await fetch(path, opts);
      if (res.status === 401) {
        sessionStorage.removeItem('kiro_admin_api_key');
        this.authenticated = false;
        this.loginKey = '';
        return;
      }
      if (!res.ok) {
        var errText = await res.text();
        try {
          var errJSON = JSON.parse(errText);
          if (errJSON && errJSON.error && errJSON.error.message) {
            throw new Error(errJSON.error.message);
          }
        } catch (parseErr) {
          if (parseErr instanceof Error && !(parseErr instanceof SyntaxError)) throw parseErr;
        }
        throw new Error(errText || 'Request failed: ' + res.status);
      }
      if (res.status === 204) return null;
      return res.json();
    },

    toast(message, type) {
      var id = ++this.toastId;
      this.toasts.push({ id: id, message: message, type: type || 'info', visible: true });
      var self = this;
      setTimeout(function() {
        self.toasts = self.toasts.filter(function(t) { return t.id !== id; });
      }, 4000);
    },

    async loadAccounts() {
      try {
        this.accounts = await this.apiCall('GET', '/admin/accounts') || [];
      } catch (e) {
        this.toast('Failed to load accounts: ' + e.message, 'error');
      }
    },

    async openDetail(id) {
      try {
        this.detailAccount = await this.apiCall('GET', '/admin/accounts/' + id);
        this.detailAccount.models = null;
        this.detailAccount.modelsLoading = false;
        this.detailAccount.testLoading = false;
        this.detailAccount.testResult = null;
        this.chatTestModel = 'claude-haiku-4.5';
        this.chatTestMessage = 'Hi';
        this.chatTestLoading = false;
        this.chatTestResult = null;
        this.currentView = 'detail';
        await this.loadAccountModels(id);
      } catch (e) {
        this.toast('Failed to load account: ' + e.message, 'error');
      }
    },

    async loadAccountModels(accountId) {
      if (!this.detailAccount) return;
      this.detailAccount.modelsLoading = true;
      try {
        var result = await this.apiCall('GET', '/admin/accounts/' + accountId + '/models');
        this.detailAccount.models = result || { models: [] };
        this.chatTestModel = this.defaultChatTestModel(this.detailAccount.models);
      } catch (e) {
        this.toast('Failed to load models: ' + e.message, 'error');
      } finally {
        this.detailAccount.modelsLoading = false;
      }
    },

    defaultChatTestModel(modelsResult) {
      var fallback = 'claude-haiku-4.5';
      if (!modelsResult) return fallback;
      if (modelsResult.default_model && modelsResult.default_model.model_id) return modelsResult.default_model.model_id;
      var models = modelsResult.models || [];
      for (var i = 0; i < models.length; i++) {
        if (models[i].is_default && models[i].model_id) return models[i].model_id;
      }
      return models.length && models[0].model_id ? models[0].model_id : fallback;
    },

    async testAccount(accountId) {
      if (!this.detailAccount) return;
      this.detailAccount.testLoading = true;
      this.detailAccount.testResult = null;
      try {
        var result = await this.apiCall('POST', '/admin/accounts/' + accountId + '/test');
        this.detailAccount.testResult = result;
        var ok = result && result.status === 'valid';
        this.toast(ok ? 'Account valid' : ('Account status: ' + this.testStatusLabel(result && result.status)), ok ? 'success' : 'warning');
      } catch (e) {
        this.toast('Test failed: ' + e.message, 'error');
      } finally {
        this.detailAccount.testLoading = false;
      }
    },

    async sendChatTest(accountId) {
      this.chatTestLoading = true;
      this.chatTestResult = null;
      try {
        var result = await this.apiCall('POST', '/admin/accounts/' + accountId + '/chat-test', {
          model: this.chatTestModel,
          message: this.chatTestMessage
        });
        this.chatTestResult = result;
        this.toast('Chat test completed', 'success');
      } catch (e) {
        this.chatTestResult = { success: false, error: e.message };
        this.toast('Chat test failed: ' + e.message, 'error');
      } finally {
        this.chatTestLoading = false;
      }
    },

    async toggleEnabled(acc) {
      if (acc.enabled) {
        this.disableAccountId = acc.id;
        this.disableReason = '';
        this.showDisableModal = true;
        return;
      }
      try {
        await this.apiCall('PATCH', '/admin/accounts/' + acc.id, { enabled: true });
        this.toast('Account enabled', 'success');
        await this.loadAccounts();
      } catch (e) {
        this.toast('Failed to enable: ' + e.message, 'error');
      }
    },

    async submitDisable() {
      this.disableLoading = true;
      try {
        await this.apiCall('PATCH', '/admin/accounts/' + this.disableAccountId, {
          enabled: false,
          disabled_reason: this.disableReason || 'Disabled via admin UI'
        });
        this.showDisableModal = false;
        this.toast('Account disabled', 'success');
        await this.loadAccounts();
      } catch (e) {
        this.toast('Failed to disable: ' + e.message, 'error');
      } finally {
        this.disableLoading = false;
      }
    },

    confirmDelete(acc) {
      this.deleteTarget = acc;
      this.showDeleteModal = true;
    },

    async submitDelete() {
      if (!this.deleteTarget) return;
      this.deleteLoading = true;
      try {
        await this.apiCall('DELETE', '/admin/accounts/' + this.deleteTarget.id);
        this.showDeleteModal = false;
        this.toast('Account deleted', 'success');
        if (this.currentView === 'detail') {
          this.currentView = 'accounts';
          this.detailAccount = null;
        }
        await this.loadAccounts();
      } catch (e) {
        this.toast('Failed to delete: ' + e.message, 'error');
      } finally {
        this.deleteLoading = false;
      }
    },

    openAddModal() {
      this.addForm = { label: '', auth_method: 'social', refresh_token: '', api_key: '', profile_arn: '', region: 'us-east-1', proxy_url: '' };
      this.showAddModal = true;
    },

    async submitAdd() {
      this.addLoading = true;
      var payload = {
        label: this.addForm.label,
        auth_method: this.addForm.auth_method,
        region: this.addForm.region,
        enabled: true
      };
      if (this.addForm.auth_method === 'social') {
        payload.refresh_token = this.addForm.refresh_token;
      } else {
        payload.api_key = this.addForm.api_key;
      }
      if (this.addForm.profile_arn) payload.profile_arn = this.addForm.profile_arn;
      if (this.addForm.proxy_url) payload.proxy_url = this.addForm.proxy_url;
      try {
        var result = await this.apiCall('POST', '/admin/accounts', payload);
        this.showAddModal = false;
        if (result && result.verified) {
          this.toast('Account created and verified successfully', 'success');
        } else if (result && !result.verified) {
          this.toast('Account created but verification failed: ' + (result.verification_error || 'unknown error') + '. Account has been disabled.', 'warning');
        } else {
          this.toast('Account created', 'success');
        }
        await this.loadAccounts();
        await this.loadQuota();
      } catch (e) {
        this.toast('Failed to create: ' + e.message, 'error');
      } finally {
        this.addLoading = false;
      }
    },

    openEditModal(acc) {
      this.editForm = {
        id: acc.id,
        label: acc.label,
        region: acc.region || '',
        proxy_url: acc.proxy_url || ''
      };
      this.showEditModal = true;
    },

    async submitEdit() {
      this.editLoading = true;
      var payload = {};
      if (this.editForm.label) payload.label = this.editForm.label;
      if (this.editForm.region) payload.region = this.editForm.region;
      if (this.editForm.proxy_url) payload.proxy_url = this.editForm.proxy_url;
      try {
        await this.apiCall('PATCH', '/admin/accounts/' + this.editForm.id, payload);
        this.showEditModal = false;
        this.toast('Account updated', 'success');
        await this.loadAccounts();
        if (this.detailAccount && this.detailAccount.account.id === this.editForm.id) {
          this.detailAccount = await this.apiCall('GET', '/admin/accounts/' + this.editForm.id);
        }
      } catch (e) {
        this.toast('Failed to update: ' + e.message, 'error');
      } finally {
        this.editLoading = false;
      }
    },

    async forceRefresh(id) {
      this.actionLoading = true;
      try {
        await this.apiCall('POST', '/admin/accounts/' + id + '/refresh');
        this.toast('Token refresh initiated', 'success');
        this.detailAccount = await this.apiCall('GET', '/admin/accounts/' + id);
      } catch (e) {
        this.toast('Refresh failed: ' + e.message, 'error');
      } finally {
        this.actionLoading = false;
      }
    },

    async loadQuota() {
      this.quotaLoading = true;
      try {
        this.quotas = await this.apiCall('GET', '/admin/quota') || [];
        var refreshing = {};
        for (var i = 0; i < this.quotas.length; i++) {
          refreshing[this.quotas[i].account_id] = false;
        }
        this.quotaRefreshing = refreshing;
      } catch (e) {
        this.toast('Failed to load quota: ' + e.message, 'error');
      } finally {
        this.quotaLoading = false;
      }
    },

    async refreshAllQuota() {
      this.quotaLoading = true;
      try {
        for (var i = 0; i < this.quotas.length; i++) {
          await this.refreshQuota(this.quotas[i].account_id);
        }
        this.toast('All quotas refreshed', 'success');
      } finally {
        this.quotaLoading = false;
      }
    },

    async refreshQuota(accountId) {
      this.quotaRefreshing = Object.assign({}, this.quotaRefreshing, { [accountId]: true });
      try {
        var result = await this.apiCall('GET', '/admin/accounts/' + accountId + '/quota?force=true');
        if (result) {
          for (var i = 0; i < this.quotas.length; i++) {
            if (this.quotas[i].account_id === accountId) {
              this.quotas[i].subscription_title = result.subscription_title;
              this.quotas[i].limit_total = result.limit_total;
              this.quotas[i].limit_remaining = result.limit_remaining;
              this.quotas[i].current_usage = result.current_usage;
              this.quotas[i].overage_cap = result.overage_cap;
              this.quotas[i].fetched_at = result.fetched_at;
              this.quotas[i].stale = false;
              break;
            }
          }
        }
        this.toast('Quota refreshed', 'success');
      } catch (e) {
        this.toast('Failed to refresh quota: ' + e.message, 'error');
      } finally {
        this.quotaRefreshing = Object.assign({}, this.quotaRefreshing, { [accountId]: false });
      }
    },

    startQuotaPoll() {
      if (this.quotaInterval) clearInterval(this.quotaInterval);
      this.quotaInterval = setInterval(() => {
        if (this.authenticated) {
          for (var i = 0; i < this.quotas.length; i++) {
            this.refreshQuota(this.quotas[i].account_id);
          }
        }
      }, 30 * 60 * 1000);
    },

    async loadHealth() {
      try {
        var res = await fetch('/health');
        if (res.ok) {
          this.health = await res.json();
        }
      } catch (e) { /* health endpoint is best-effort */ }
    },

    startHealthPoll() {
      var self = this;
      this.healthInterval = setInterval(function() { self.loadHealth(); }, 30000);
    },

    formatTime(val) {
      if (!val) return '-';
      try {
        var d = new Date(val);
        return d.toLocaleString();
      } catch (e) {
        return val;
      }
    },

    isSecretField(key) {
      return key === 'access_token' || key === 'refresh_token' || key === 'api_key';
    },

    formatRate(model) {
      if (!model) return '-';
      var mult = model.rate_multiplier != null ? model.rate_multiplier : 0;
      return mult + ' ' + (model.rate_unit || '').trim();
    },

    formatTokenLimits(model) {
      if (!model || !model.token_limits) return '-';
      var input = model.token_limits.max_input_tokens || 0;
      var output = model.token_limits.max_output_tokens || 0;
      return input.toLocaleString() + ' in / ' + output.toLocaleString() + ' out';
    },

    testStatusLabel(status) {
      var labels = {
        valid: 'Valid',
        banned: 'Banned',
        suspended: 'Suspended',
        token_expired: 'Token Expired',
        error: 'Error'
      };
      return labels[status] || 'Unknown';
    },

    testStatusClass(status) {
      if (status === 'valid') return 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30';
      if (status === 'suspended') return 'bg-amber-500/15 text-amber-400 border-amber-500/30';
      if (status === 'banned' || status === 'token_expired') return 'bg-red-500/15 text-red-400 border-red-500/30';
      return 'bg-slate-700/60 text-slate-300 border-slate-600';
    }
  };
}
