FROM nginx:1.27-alpine

RUN rm /etc/nginx/conf.d/default.conf

COPY nginx/nginx.conf /etc/nginx/conf.d/default.conf

COPY app/frontend/index.html /usr/share/nginx/html/index.html
COPY app/frontend/style.css /usr/share/nginx/html/style.css
COPY app/frontend/script.js /usr/share/nginx/html/script.js

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
