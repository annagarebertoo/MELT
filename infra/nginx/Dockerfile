FROM nginx:1.28-alpine

COPY nginx.render.conf /etc/nginx/nginx.conf
COPY default.render.conf /etc/nginx/conf.d/default.conf
COPY web/ /usr/share/nginx/html/

EXPOSE 10000

CMD ["nginx", "-g", "daemon off;"]
