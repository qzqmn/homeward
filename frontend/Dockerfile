FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
# dist/ 由 CI 在建置前產生（Expo 專案存在時），否則使用佔位頁
COPY dist/ /usr/share/nginx/html/
