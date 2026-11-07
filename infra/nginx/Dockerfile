FROM nginx:1.28-alpine

COPY nginx.conf /etc/nginx/nginx.conf
COPY default.render.conf /etc/nginx/conf.d/default.conf

EXPOSE 10000

CMD ["nginx", "-g", "daemon off;"]