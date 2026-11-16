axios.defaults.headers.post['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8';
axios.defaults.headers.common['X-Requested-With'] = 'XMLHttpRequest';

axios.interceptors.response.use(response => {
    const token = response.headers && response.headers['x-csrf-token'];
    if (token) axios.defaults.headers.common['X-CSRF-Token'] = token;
    return response;
});

axios.interceptors.request.use(
    config => {
        config.data = Qs.stringify(config.data, {
            arrayFormat: 'repeat'
        });
        return config;
    },
    error => Promise.reject(error)
);
