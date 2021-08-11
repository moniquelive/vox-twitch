let token, userId;
let options = [];

// so we don't have to write this out everytime #efficency
const twitch = window.Twitch.ext;


// callback called when context of an extension is fired
twitch.onContext((context) => {
    //console.log(context);
});


// onAuthorized callback called each time JWT is fired
twitch.onAuthorized((auth) => {
    // save our credentials
    token = auth.token; //JWT passed to backend for authentication
    userId = auth.userId; //opaque userID
});

// when the config changes, update the panel!
twitch.configuration.onChanged(() => {
    //console.log(twitch.configuration.broadcaster)
    if (twitch.configuration.broadcaster) {
        try {
            var config = JSON.parse(twitch.configuration.broadcaster.content)
            //console.log(typeof config)
            if (typeof config === "object") {
                options = config
                updateOptions()
            } else {
                //console.log('invalid config')
            }
        } catch (e) {
            //console.log('invalid config')
        }
    }
})


$(() => {
    $("#form").submit((e) => {
        e.preventDefault();
        //console.log('in function')
        if (!token) {
            return //console.log('Not authorized');
        }
        //console.log('Submitting a question');
        const $text = $("#text")
        const text = $text.val()

        let url = 'https://vox-twitch.monique.dev/tts/'
        if (document.location.hostname === 'localhost') {
            url = location.protocol + '//localhost:7001/tts/'
        }
        //ajax call
        $.ajax({
            type: 'POST',
            url: url,
            data: {text: text},
            headers: {"Authorization": 'Bearer ' + token},
            complete: () => {
                $text.val('')
            },
        });
    })
});

