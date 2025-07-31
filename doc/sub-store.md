# sub-store 后端使用教程

> ~~当你打开`http://127.0.0.1:8299`，你发现跳转到了`https://sub-store.vercel.app/subs`~~ 新版已经变化了

> 不要惊慌！不要惊慌！不要惊慌！

> 程序没有问题，你也没有问题，sub-store是一个前后端分离的项目，你直接访问了后端，他帮你跳转到了公开的前端页面。

> 所以你只需要配置一下，后端地址，你就可以直接操作后端了。
## 直接访问，会跳到这里
> 如果你网络不好，这个页面可能都打不开

![步骤一](./images/sub-store1.png)

## 右下角设置里，后端设置
![步骤二](./images/sub-store2.png)

## 填入名称和后端地址保存
![步骤三](./images/sub-store3.png)

## 正常应该会有错误（这是因为浏览器不允许https的前端访问http的后端）

> **chrome内核浏览器的解决方案** 其他浏览器也差不多是这种问题，自行谷歌解决方案

> 小一佬提供的解决方案： HTTPS 前端无法请求非本地的 HTTP 后端(部分浏览器上也无法访问本地 HTTP 后端). 请配置反代或在局域网自建 HTTP 前端.

![](./images/sub-store7.png)
![](./images/sub-store8.png)
![](./images/sub-store9.png)
## 切换到你添加的后端
![步骤四](./images/sub-store4.png)

## 订阅管理页面
> 那些不带规则的订阅就从这里出来的

![步骤五](./images/sub-store5.png)

## 文件管理页面
> 带规则的mihomo.yaml文件就从这里出来的

![步骤六](./images/sub-store6.png)

## 想要DIY？

在原基础上新建订阅或者文件，不要在subs-check专用的配置上修改！！！

## 安全/自定义PATH
> 你担心安全问题，就修改配置里的自定义path
```bash
# sub-store自定义访问路径，必须以/开头，后续访问订阅也要带上此路径
# 设置path之后，还可以开启订阅分享功能，无需暴露真实的path
# sub-store-path: "/path"
sub-store-path: "/diypath"
```
访问路径变成`http://127.0.0.1:8299/diypath`

![步骤十](./images/sub-store10.png)
![步骤十一](./images/sub-store11.png)