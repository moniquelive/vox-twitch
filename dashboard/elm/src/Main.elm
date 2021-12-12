port module Main exposing (..)

import Animation exposing (percent, px)
import Animation.Messenger
import Browser
import Dict exposing (Dict)
import Html exposing (..)
import Html.Attributes exposing (..)
import Json.Decode as D
import Time



-- MAIN


main : Program Float Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }



-- PORTS


port playUrl : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg


port audioEnded : (String -> msg) -> Sub msg



-- MODEL


type alias WebsocketMessage =
    { client_id : String
    , audio_url : String
    , text : String
    , emotes : Emotes
    , username : String
    , user_picture : String
    }


type alias Card =
    { username : String
    , text : String
    , emotes : Emotes
    , user_picture : String
    , audio_url : String
    , animStyle : Animation.Messenger.State Msg
    }


type alias Model =
    { cards : List Card
    , audios : List String
    , innerHeight : Float
    }


type alias Emotes =
    Dict String String


init : Float -> ( Model, Cmd Msg )
init innerHeight =
    ( Model [] [] innerHeight, Cmd.none )



-- UPDATE


type Msg
    = WebsocketMessageReceived String
    | Animate Animation.Msg
    | AnimationDone
    | AudioEnded String


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Animate animMsg ->
            let
                stylesAndCmds =
                    List.map (\card -> Animation.Messenger.update animMsg card.animStyle) model.cards

                styles =
                    List.map Tuple.first stylesAndCmds

                cmds =
                    List.map Tuple.second stylesAndCmds

                updateStyle card style =
                    { card | animStyle = style }
            in
            ( { model | cards = List.map2 updateStyle model.cards styles }
            , Cmd.batch cmds
            )

        WebsocketMessageReceived json ->
            case D.decodeString websocketMessageDecoder json of
                Ok ws ->
                    let
                        newAnimation =
                            Animation.interrupt
                                [ Animation.to [ Animation.translate (percent 0) (px 0) ]
                                ]
                                (Animation.style [ Animation.translate (percent 0) (px model.innerHeight) ])

                        newCard =
                            [ Card ws.username ws.text ws.emotes ws.user_picture ws.audio_url newAnimation ]

                        newAudio =
                            if String.isEmpty ws.audio_url then
                                []

                            else
                                [ ws.audio_url ]

                        cmd =
                            if List.isEmpty model.audios then
                                playUrl ws.audio_url

                            else
                                Cmd.none
                    in
                    ( { model
                        | cards = model.cards ++ newCard
                        , audios = model.audios ++ newAudio
                      }
                    , cmd
                    )

                Err _ ->
                    ( model, Cmd.none )

        AnimationDone ->
            ( { model | cards = List.drop 1 model.cards }, Cmd.none )

        AudioEnded audio_url ->
            let
                newCards =
                    List.map
                        (\card ->
                            if card.audio_url == audio_url then
                                { card
                                    | animStyle =
                                        Animation.queue
                                            [ Animation.wait (Time.millisToPosix <| 3 * 1000)
                                            , Animation.to [ Animation.translate (percent 115) (percent 0) ]
                                            , Animation.Messenger.send AnimationDone
                                            ]
                                            card.animStyle
                                }

                            else
                                card
                        )
                        model.cards

                newAudios =
                    List.drop 1 model.audios

                cmd =
                    case List.head newAudios of
                        Just url ->
                            playUrl url

                        Nothing ->
                            Cmd.none
            in
            ( { model | audios = newAudios, cards = newCards }, cmd )



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions model =
    Sub.batch
        [ messageReceiver WebsocketMessageReceived
        , audioEnded AudioEnded
        , Animation.subscription Animate <| List.map .animStyle model.cards
        ]



-- VIEW


cardView : Card -> Html Msg
cardView card =
    div
        (Animation.render card.animStyle
            ++ [ class "content" ]
        )
        [ div [ class "user-picture" ] [ img [ src card.user_picture ] [] ]
        , div [ class "container" ]
            [ div [ class "username" ] [ text <| card.username ++ " disse:" ]
            , div [ class "text" ] (filterEmote card.emotes card.text)
            ]
        ]


filterEmote : Emotes -> String -> List (Html msg)
filterEmote emotes text =
    text
        |> String.split " "
        |> List.map (mapUrl emotes)


mapUrl : Emotes -> String -> Html msg
mapUrl emotes word =
    let
        urlTemplate =
            "https://cdn.betterttv.net/emote/"
    in
    Dict.get word emotes
        |> Maybe.map (\w -> urlTemplate ++ w ++ "/1x")
        |> Maybe.map (\url -> img [ src url ] [])
        |> Maybe.withDefault (text (" " ++ word ++ " "))


view : Model -> Html Msg
view model =
    div [ class "main" ] (List.map cardView model.cards)



-- JSON decode


websocketMessageDecoder : D.Decoder WebsocketMessage
websocketMessageDecoder =
    D.map6 WebsocketMessage
        (D.field "client_id" D.string)
        (D.field "audio_url" D.string)
        (D.field "text" D.string)
        (D.field "emotes" (D.dict D.string))
        (D.field "username" D.string)
        (D.field "user_picture" D.string)
