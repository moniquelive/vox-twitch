const twitch = window.Twitch.ext;
const target = document.createElement('div')
document.body.appendChild(target)
const app = Elm.Main.init({node: target})
app.ports.requestIdShare.subscribe(function () {
    if (!twitch.viewer.isLinked) {
        twitch.actions.requestIdShare();
    }
})

// callback called when context of an extension is fired
twitch.onContext((context) => {
    //console.log(context);
});
// onAuthorized callback called each time JWT is fired
twitch.onAuthorized((auth) => {
    app.ports.onAuthorized.send([auth.token, auth.channelId]);
});

// when the config changes, update the panel!
// twitch.configuration.onChanged(() => {
//     //console.log(twitch.configuration.broadcaster)
//     if (twitch.configuration.broadcaster) {
//         try {
//             var config = JSON.parse(twitch.configuration.broadcaster.content)
//             //console.log(typeof config)
//             if (typeof config === "object") {
//                 options = config
//                 updateOptions()
//             } else {
//                 //console.log('invalid config')
//             }
//         } catch (e) {
//             //console.log('invalid config')
//         }
//     }
// })
